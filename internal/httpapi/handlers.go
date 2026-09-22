package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ieyoukan/pplale-cms/internal/api"
	"github.com/ieyoukan/pplale-cms/internal/auth"
	"github.com/ieyoukan/pplale-cms/internal/cards"
	"github.com/ieyoukan/pplale-cms/internal/imageconv"
	"github.com/ieyoukan/pplale-cms/internal/publish"
	"github.com/ieyoukan/pplale-cms/internal/store"
)

// maxUploadBytes bounds the whole multipart submission.
const maxUploadBytes = imageconv.MaxSourceBytes + (1 << 20)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.deps.DevSkipAuth {
		if !s.allowLogin(w, r, s.deps.DevUser.ID) {
			return
		}
		if _, err := s.deps.Sessions.Issue(r.Context(), w, s.deps.DevUser); err != nil {
			writeError(w, http.StatusInternalServerError, "セッションを作成できませんでした")
			return
		}
		s.deps.Logger.Warn("dev-skip-auth login", "discord_id", s.deps.DevUser.ID)
		http.Redirect(w, r, auth.SafeReturnPath(r.URL.Query().Get("return_to")), http.StatusFound)
		return
	}

	pkce, err := auth.NewPKCE()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ログインを開始できませんでした")
		return
	}
	cookie, state, err := s.deps.StateSigner.Issue(pkce, auth.SafeReturnPath(r.URL.Query().Get("return_to")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ログインを開始できませんでした")
		return
	}
	s.deps.Sessions.SetStateCookie(w, cookie)
	http.Redirect(w, r, s.deps.Discord.AuthorizeURL(state, pkce), http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(auth.StateCookie)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ログインの状態が見つかりません。もう一度お試しください")
		return
	}
	s.deps.Sessions.ClearStateCookie(w)

	state, err := s.deps.StateSigner.Open(stateCookie.Value, r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, err := s.deps.Discord.Exchange(r.Context(), r.URL.Query().Get("code"), state.Verifier)
	if err != nil {
		s.deps.Logger.Warn("discord exchange failed", "err", err)
		writeError(w, http.StatusUnauthorized, "Discord 認証に失敗しました")
		return
	}
	if !s.allowLogin(w, r, user.ID) {
		return
	}

	if _, err := s.deps.Sessions.Issue(r.Context(), w, user); err != nil {
		writeError(w, http.StatusInternalServerError, "セッションを作成できませんでした")
		return
	}
	s.deps.Logger.Info("login", "discord_id", user.ID, "name", user.DisplayName())
	http.Redirect(w, r, auth.SafeReturnPath(state.ReturnTo), http.StatusFound)
}

// allowLogin makes the allow list the login boundary, not merely the submit
// boundary. The middleware still checks the entry on every request so that a
// user removed after login loses access immediately.
func (s *Server) allowLogin(w http.ResponseWriter, r *http.Request, discordID string) bool {
	if _, err := s.deps.Store.GetUser(r.Context(), discordID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.deps.Logger.Warn("login denied: user is not on allow list", "discord_id", discordID)
			writeError(w, http.StatusForbidden, "この Discord ユーザーはログインを許可されていません。管理者に許可リストへの追加を依頼してください")
			return false
		}
		s.deps.Logger.Error("login allow list lookup failed", "discord_id", discordID, "err", err)
		writeError(w, http.StatusInternalServerError, "ログイン権限を確認できませんでした")
		return false
	}
	return true
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Sessions.Revoke(r.Context(), w, r); err != nil {
		writeError(w, http.StatusInternalServerError, "ログアウトに失敗しました")
		return
	}
	writeJSON(w, http.StatusOK, api.StatusResponse{Status: "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	resp := api.Me{DiscordID: session.DiscordID, DisplayName: session.DisplayName}
	if user, ok := userFrom(r.Context()); ok {
		resp.Role = user.Role
		resp.CanSubmit = user.Role.CanSubmit()
		resp.CanManageUsers = user.Role.CanManageUsers()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDatasets hands the form its option lists. Every value comes from the
// cards package, so the UI cannot offer something validation would reject.
func (s *Server) handleDatasets(w http.ResponseWriter, _ *http.Request) {
	labels := map[cards.Kind]string{
		cards.KindYojo: "幼女", cards.KindSweet: "お菓子",
		cards.KindPlayable: "プレイアブル", cards.KindTokenYojo: "トークン幼女",
	}

	datasets := make([]api.Dataset, 0, len(cards.AllKinds()))
	for _, kind := range cards.AllKinds() {
		ds, err := cards.DatasetFor(kind)
		if err != nil {
			continue
		}
		datasets = append(datasets, api.Dataset{
			Kind: string(kind), Label: labels[kind], IDPrefix: ds.IDPrefix,
			CardType: string(ds.CardType), ImageDir: ds.ImageDir,
		})
	}

	writeJSON(w, http.StatusOK, api.Metadata{
		Datasets:   datasets,
		Fruits:     enumStrings(cards.AllFruits()),
		Roles:      enumStrings(cards.AllRoles()),
		SweetTypes: enumStrings(cards.AllSweetTypes()),
		Versions:   enumStrings(cards.AllVersions()),
	})
}

func enumStrings[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

// handleCards proxies the live upstream dataset so the editor always works
// against what is actually on main.
func (s *Server) handleCards(w http.ResponseWriter, r *http.Request) {
	ds, err := cards.DatasetFor(cards.Kind(r.URL.Query().Get("kind")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "不明なデータセットです")
		return
	}
	raw, err := s.deps.CardReader.FileContent(r.Context(), ds.JSONPath)
	if err != nil {
		s.deps.Logger.Error("fetch dataset", "path", ds.JSONPath, "err", err)
		writeError(w, http.StatusBadGateway, "PPLALE-web からカードデータを取得できませんでした")
		return
	}
	list, err := cards.Decode(ds, raw)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	out := make([]api.Card, len(list))
	for i, c := range list {
		out[i] = s.cardToAPI(c)
	}
	writeJSON(w, http.StatusOK, api.CardsResponse{Kind: string(ds.Kind), NextID: cards.NextID(ds, list), Cards: out})
}

func (s *Server) cardToAPI(c cards.Card) api.Card {
	return api.Card{
		ID: c.ID, Name: c.Name, Type: string(c.Type), Fruit: string(c.Fruit),
		Description: c.Description, ImageURL: c.ImageURL,
		ImageDisplayURL: s.deps.CardReader.RawURL(cards.RepoImagePath(c.ImageURL)),
		Cost:            c.Cost, HP: c.HP, Attack: c.Attack,
		Effect: c.Effect, Role: stringPtr(c.Role), SweetType: stringPtr(c.SweetType), Version: stringPtr(c.Version),
	}
}

// buildDraftCard turns a submitted payload into the internal card shape,
// applying dataset defaults so a draft always has the same optional fields
// its neighbours in the file would.
func buildDraftCard(ds cards.Dataset, payload api.SubmitPayload) cards.Card {
	card := cards.Card{
		ID:          strings.TrimSpace(payload.ID),
		Name:        strings.TrimSpace(payload.Name),
		Type:        ds.CardType,
		Fruit:       cards.FruitType(payload.Fruit),
		Description: payload.Description,
		Cost:        payload.Cost,
		HP:          payload.HP,
		Attack:      payload.Attack,
		Effect:      payload.Effect,
		Role:        rolePtr(payload.Role),
		SweetType:   sweetPtr(payload.SweetType),
		Version:     versionPtr(payload.Version),
	}
	cards.ApplyDatasetDefaults(ds, &card)
	return card
}

// handleCreateDraft converts an uploaded image immediately (imageconv is
// deterministic, so there is no reason to defer it) and queues the card for
// later batch submission. Nothing reaches PPLALE-web yet.
func (s *Server) handleCreateDraft(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "アップロードを読み取れませんでした")
		return
	}
	defer r.MultipartForm.RemoveAll()

	var payload api.SubmitPayload
	if err := json.Unmarshal([]byte(r.FormValue("payload")), &payload); err != nil {
		writeError(w, http.StatusBadRequest, "payload の JSON が不正です")
		return
	}

	ds, err := cards.DatasetFor(cards.Kind(payload.Kind))
	if err != nil {
		writeError(w, http.StatusBadRequest, "不明なデータセットです")
		return
	}

	var imageBytes []byte
	if file, header, err := r.FormFile("image"); err == nil {
		defer file.Close()
		if header.Size > imageconv.MaxSourceBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "画像が大きすぎます")
			return
		}
		imageBytes, err = io.ReadAll(io.LimitReader(file, imageconv.MaxSourceBytes+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "画像を読み取れませんでした")
			return
		}
	} else if !errors.Is(err, http.ErrMissingFile) {
		writeError(w, http.StatusBadRequest, "画像を読み取れませんでした")
		return
	}

	card := buildDraftCard(ds, payload)
	isEdit := card.ID != ""

	var converted *imageconv.Result
	var imageSlug string
	switch {
	case len(imageBytes) > 0:
		slug, err := generateImageSlug()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		imageSlug = slug
		result, err := imageconv.Convert(imageBytes)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, api.ErrorResponse{
				Error: "入力内容を確認してください", Fields: map[string]string{"imageUrl": err.Error()}})
			return
		}
		converted = &result
		card.ImageURL = ds.ImageURL(slug + ".webp")
	case !isEdit:
		writeJSON(w, http.StatusUnprocessableEntity, api.ErrorResponse{
			Error: "入力内容を確認してください", Fields: map[string]string{"imageUrl": "新規カードには画像が必要です"}})
		return
	default:
		// Edit without a new image: validate against a placeholder URL. The
		// authoritative check against the real existing image happens again
		// at publish time, against live upstream data.
		card.ImageURL = ds.ImageURL("placeholder.webp")
	}

	if err := cards.Validate(ds, placeholderIDForValidation(ds, card)); err != nil {
		var invalid cards.ValidationErrors
		if errors.As(err, &invalid) {
			fields := map[string]string{}
			for _, e := range invalid {
				fields[e.Field] = e.Message
			}
			writeJSON(w, http.StatusUnprocessableEntity, api.ErrorResponse{Error: "入力内容を確認してください", Fields: fields})
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	draft := store.Draft{
		DiscordID:   session.DiscordID,
		DisplayName: session.DisplayName,
		Kind:        string(ds.Kind),
		CardID:      card.ID,
		Name:        card.Name,
		Fruit:       string(card.Fruit),
		Description: card.Description,
		Cost:        card.Cost,
		HP:          card.HP,
		Attack:      card.Attack,
		Effect:      card.Effect,
		Role:        stringPtr(card.Role),
		SweetType:   stringPtr(card.SweetType),
		Version:     stringPtr(card.Version),
		ImageSlug:   imageSlug,
	}
	if converted != nil {
		draft.WebP = converted.WebP
		draft.OGPPNG = converted.OGPNG
		draft.SourceBytes = converted.SourceBytes
		draft.SourceType = converted.SourceType
	}

	saved, err := s.deps.Store.CreateDraft(r.Context(), draft)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "下書きを保存できませんでした")
		return
	}

	s.deps.Logger.Info("draft created", "discord_id", session.DiscordID, "draft_id", saved.ID, "kind", ds.Kind)
	writeJSON(w, http.StatusCreated, s.draftToAPI(r.Context(), saved))
}

// generateImageSlug picks the file name a new image is stored under. Callers
// never choose this themselves: the directory is already implied by the
// dataset, and a random name sidesteps collisions without asking anyone to
// think about paths.
func generateImageSlug() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("画像ファイル名を生成できませんでした: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// placeholderIDForValidation supplies a syntactically valid ID for the fields
// Validate needs when a new card has not been assigned one yet; the real ID
// is allocated against live data at publish time.
func placeholderIDForValidation(ds cards.Dataset, card cards.Card) cards.Card {
	if card.ID == "" {
		card.ID = cards.NextID(ds, nil)
	}
	return card
}

func (s *Server) handleListDrafts(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	drafts, err := s.deps.Store.ListDraftsByUser(r.Context(), session.DiscordID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "下書きを取得できませんでした")
		return
	}

	out := make([]api.Draft, len(drafts))
	for i, d := range drafts {
		out[i] = s.draftToAPI(r.Context(), d)
	}
	writeJSON(w, http.StatusOK, api.DraftsResponse{Drafts: out})
}

// draftToAPI resolves a display URL for a draft's image: the freshly
// converted preview when one was uploaded, otherwise the live upstream image
// for an edit-in-place draft.
func (s *Server) draftToAPI(ctx context.Context, d store.Draft) api.Draft {
	out := api.Draft{
		ID: d.ID, Kind: d.Kind, CardID: d.CardID, IsEdit: d.CardID != "",
		Name: d.Name, Fruit: d.Fruit, Description: d.Description,
		Cost: d.Cost, HP: d.HP, Attack: d.Attack,
		Effect: d.Effect, Role: d.Role, SweetType: d.SweetType, Version: d.Version,
		HasNewImage: len(d.WebP) > 0, CreatedAt: d.CreatedAt,
	}
	if out.HasNewImage {
		out.ImageDisplayURL = fmt.Sprintf("/api/drafts/%d/image", d.ID)
	}
	if out.IsEdit {
		if ds, err := cards.DatasetFor(cards.Kind(d.Kind)); err == nil {
			if raw, err := s.deps.CardReader.FileContent(ctx, ds.JSONPath); err == nil {
				if list, err := cards.Decode(ds, raw); err == nil {
					for _, c := range list {
						if c.ID == d.CardID {
							original := s.cardToAPI(c)
							out.Original = &original
							if !out.HasNewImage {
								out.ImageDisplayURL = original.ImageDisplayURL
							}
							break
						}
					}
				}
			}
		}
	}
	return out
}

func (s *Server) handleDeleteDraft(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	draft, ok := s.ownedDraft(w, r, session)
	if !ok {
		return
	}
	if err := s.deps.Store.DeleteDrafts(r.Context(), []int64{draft.ID}); err != nil {
		writeError(w, http.StatusInternalServerError, "下書きを削除できませんでした")
		return
	}
	writeJSON(w, http.StatusOK, api.StatusResponse{Status: "ok"})
}

func (s *Server) handleDraftImage(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	draft, ok := s.ownedDraft(w, r, session)
	if !ok {
		return
	}
	if len(draft.WebP) == 0 {
		writeError(w, http.StatusNotFound, "画像がありません")
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(draft.WebP)
}

func (s *Server) handleDraftOGImage(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	draft, ok := s.ownedDraft(w, r, session)
	if !ok {
		return
	}
	if len(draft.OGPPNG) == 0 {
		writeError(w, http.StatusNotFound, "画像がありません")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(draft.OGPPNG)
}

// ownedDraft loads the {draftID} path parameter and confirms it belongs to
// the caller (admins may also manage any draft), writing an error response
// and returning ok=false otherwise.
func (s *Server) ownedDraft(w http.ResponseWriter, r *http.Request, session auth.Session) (store.Draft, bool) {
	id, err := strconv.ParseInt(r.PathValue("draftID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "不正な下書きIDです")
		return store.Draft{}, false
	}
	draft, err := s.deps.Store.GetDraft(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "下書きが見つかりません")
		return store.Draft{}, false
	}
	user, _ := userFrom(r.Context())
	if draft.DiscordID != session.DiscordID && !user.Role.CanManageUsers() {
		writeError(w, http.StatusForbidden, "この下書きを操作する権限がありません")
		return store.Draft{}, false
	}
	return draft, true
}

// handleSubmitDrafts publishes some or all of the caller's queued drafts as a
// single pull request.
func (s *Server) handleSubmitDrafts(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	if !s.limiter.Allow(session.DiscordID) {
		writeError(w, http.StatusTooManyRequests, "提出が多すぎます。しばらく待ってから再度お試しください")
		return
	}

	var body api.SubmitDraftsRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "リクエストが不正です")
			return
		}
	}

	drafts, err := s.deps.Store.ListDraftsByUser(r.Context(), session.DiscordID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "下書きを取得できませんでした")
		return
	}
	if len(body.IDs) > 0 {
		wanted := make(map[int64]bool, len(body.IDs))
		for _, id := range body.IDs {
			wanted[id] = true
		}
		filtered := drafts[:0]
		for _, d := range drafts {
			if wanted[d.ID] {
				filtered = append(filtered, d)
			}
		}
		drafts = filtered
	}
	if len(drafts) == 0 {
		writeError(w, http.StatusBadRequest, "提出するカードがありません")
		return
	}

	items := make([]publish.Item, len(drafts))
	for i, d := range drafts {
		card := cards.Card{
			ID: d.CardID, Name: d.Name, Fruit: cards.FruitType(d.Fruit), Description: d.Description,
			Cost: d.Cost, HP: d.HP, Attack: d.Attack, Effect: d.Effect,
			Role: rolePtr(d.Role), SweetType: sweetPtr(d.SweetType), Version: versionPtr(d.Version),
		}
		item := publish.Item{Kind: cards.Kind(d.Kind), Card: card, ImageSlug: d.ImageSlug}
		if len(d.WebP) > 0 {
			item.Image = &imageconv.Result{
				WebP: d.WebP, OGPNG: d.OGPPNG, SourceBytes: d.SourceBytes, SourceType: d.SourceType,
			}
		}
		items[i] = item
	}

	result, err := s.deps.Publisher.PublishBatch(r.Context(), items, publish.Submitter{
		DiscordID: session.DiscordID, DiscordName: session.DisplayName,
	})
	if err != nil {
		var invalid cards.ValidationErrors
		if errors.As(err, &invalid) {
			fields := map[string]string{}
			for _, e := range invalid {
				fields[e.Field] = e.Message
			}
			writeJSON(w, http.StatusUnprocessableEntity, api.ErrorResponse{Error: "入力内容を確認してください", Fields: fields})
			return
		}
		s.deps.Logger.Error("publish batch failed", "discord_id", session.DiscordID, "err", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("PR の作成に失敗しました: %v", err))
		return
	}

	submissionCards := make([]api.SubmissionCard, len(result.Cards))
	for i, c := range result.Cards {
		submissionCards[i] = api.SubmissionCard{Kind: string(c.Kind), CardID: c.CardID, CardName: c.CardName, IsEdit: c.IsEdit}
	}
	record, err := s.deps.Store.CreateSubmission(r.Context(), store.Submission{
		DiscordID: session.DiscordID, DisplayName: session.DisplayName,
		Branch: result.Branch, PRNumber: result.PullRequest.Number, PRURL: result.PullRequest.HTMLURL,
		Status: store.StatusOpen, Cards: submissionCards,
	})
	if err != nil {
		// The pull request exists upstream; losing the audit row must not look
		// like a failed submission.
		s.deps.Logger.Error("audit write failed", "pr", result.PullRequest.Number, "err", err)
	}

	submittedIDs := make([]int64, len(drafts))
	for i, d := range drafts {
		submittedIDs[i] = d.ID
	}
	if err := s.deps.Store.DeleteDrafts(r.Context(), submittedIDs); err != nil {
		s.deps.Logger.Error("draft cleanup failed", "err", err)
	}

	s.deps.Logger.Info("batch published",
		"discord_id", session.DiscordID, "cards", len(result.Cards), "pr", result.PullRequest.HTMLURL)
	writeJSON(w, http.StatusCreated, api.SubmitResult{
		Branch: result.Branch, Files: result.Files,
		PRURL: result.PullRequest.HTMLURL, PRNumber: result.PullRequest.Number,
		Submission: &record,
	})
}

func (s *Server) handleSubmissions(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	list, err := s.deps.Store.ListSubmissions(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "履歴を取得できませんでした")
		return
	}
	writeJSON(w, http.StatusOK, api.SubmissionsResponse{Submissions: list})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "許可リストを取得できませんでした")
		return
	}
	writeJSON(w, http.StatusOK, api.UsersResponse{Users: list})
}

func (s *Server) handleUpsertUser(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	discordID := r.PathValue("discordID")
	if !validDiscordID(discordID) {
		writeError(w, http.StatusBadRequest, "Discord ユーザーIDの形式が不正です")
		return
	}

	var body api.UpsertUserRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストが不正です")
		return
	}
	role := body.Role
	if !role.Valid() {
		writeError(w, http.StatusBadRequest, "role は creator か admin である必要があります")
		return
	}

	if err := s.deps.Store.UpsertUser(r.Context(), store.User{
		DiscordID:   discordID,
		DisplayName: strings.TrimSpace(body.DisplayName),
		Role:        role,
		AddedBy:     session.DiscordID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "許可リストを更新できませんでした")
		return
	}
	s.deps.Logger.Info("allowlist upsert", "by", session.DiscordID, "target", discordID, "role", role)
	writeJSON(w, http.StatusOK, api.StatusResponse{Status: "ok"})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	discordID := r.PathValue("discordID")
	if discordID == session.DiscordID {
		writeError(w, http.StatusBadRequest, "自分自身は削除できません")
		return
	}
	if err := s.deps.Store.DeleteUser(r.Context(), discordID); err != nil {
		writeError(w, http.StatusInternalServerError, "許可リストを更新できませんでした")
		return
	}
	// Access is withdrawn immediately, including from browsers already open.
	if err := s.deps.Sessions.RevokeAllFor(r.Context(), discordID); err != nil {
		s.deps.Logger.Error("session revoke failed", "target", discordID, "err", err)
	}
	s.deps.Logger.Info("allowlist delete", "by", session.DiscordID, "target", discordID)
	writeJSON(w, http.StatusOK, api.StatusResponse{Status: "ok"})
}

func validDiscordID(id string) bool {
	if len(id) < 15 || len(id) > 25 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func stringPtr[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

func rolePtr(v *string) *cards.CardRole {
	if v == nil {
		return nil
	}
	r := cards.CardRole(*v)
	return &r
}

func sweetPtr(v *string) *cards.SweetType {
	if v == nil {
		return nil
	}
	s := cards.SweetType(*v)
	return &s
}

func versionPtr(v *string) *cards.CardVersion {
	if v == nil {
		return nil
	}
	c := cards.CardVersion(*v)
	return &c
}
