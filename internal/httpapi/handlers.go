package httpapi

import (
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

	if _, err := s.deps.Sessions.Issue(r.Context(), w, user); err != nil {
		writeError(w, http.StatusInternalServerError, "セッションを作成できませんでした")
		return
	}
	s.deps.Logger.Info("login", "discord_id", user.ID, "name", user.DisplayName())
	http.Redirect(w, r, auth.SafeReturnPath(state.ReturnTo), http.StatusFound)
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
		out[i] = api.Card{
			ID: c.ID, Name: c.Name, Type: string(c.Type), Fruit: string(c.Fruit),
			Description: c.Description, ImageURL: c.ImageURL,
			ImageDisplayURL: s.deps.CardReader.RawURL(cards.RepoImagePath(c.ImageURL)),
			Cost:            c.Cost, HP: c.HP, Attack: c.Attack,
			Effect: c.Effect, Role: stringPtr(c.Role), SweetType: stringPtr(c.SweetType), Version: stringPtr(c.Version),
		}
	}
	writeJSON(w, http.StatusOK, api.CardsResponse{Kind: string(ds.Kind), NextID: cards.NextID(ds, list), Cards: out})
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	if !s.limiter.Allow(session.DiscordID) {
		writeError(w, http.StatusTooManyRequests, "提出が多すぎます。しばらく待ってから再度お試しください")
		return
	}

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

	card := cards.Card{
		ID:          strings.TrimSpace(payload.ID),
		Name:        strings.TrimSpace(payload.Name),
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

	result, err := s.deps.Publisher.Publish(r.Context(), publish.Submission{
		Kind:        ds.Kind,
		Card:        card,
		ImageSlug:   payload.ImageSlug,
		ImageSource: imageBytes,
		Submitter: publish.Submitter{
			DiscordID:   session.DiscordID,
			DiscordName: session.DisplayName,
		},
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
		s.deps.Logger.Error("publish failed", "discord_id", session.DiscordID, "err", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("PR の作成に失敗しました: %v", err))
		return
	}

	record, err := s.deps.Store.CreateSubmission(r.Context(), store.Submission{
		DiscordID:   session.DiscordID,
		DisplayName: session.DisplayName,
		Kind:        string(ds.Kind),
		CardID:      result.CardID,
		CardName:    card.Name,
		Branch:      result.Branch,
		PRNumber:    result.PullRequest.Number,
		PRURL:       result.PullRequest.HTMLURL,
		Status:      store.StatusOpen,
	})
	if err != nil {
		// The pull request exists upstream; losing the audit row must not look
		// like a failed submission.
		s.deps.Logger.Error("audit write failed", "pr", result.PullRequest.Number, "err", err)
	}

	s.deps.Logger.Info("submission published",
		"discord_id", session.DiscordID, "card", result.CardID, "pr", result.PullRequest.HTMLURL)
	writeJSON(w, http.StatusCreated, api.SubmitResult{
		CardID:     result.CardID,
		Branch:     result.Branch,
		Files:      result.Files,
		PRURL:      result.PullRequest.HTMLURL,
		PRNumber:   result.PullRequest.Number,
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
