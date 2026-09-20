package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ieyoukan/pplale-cms/internal/api"
	"github.com/ieyoukan/pplale-cms/internal/auth"
	"github.com/ieyoukan/pplale-cms/internal/ghapp"
	"github.com/ieyoukan/pplale-cms/internal/publish"
	"github.com/ieyoukan/pplale-cms/internal/store"
)

const yojoFixture = `{
  "yojo": [
    {
      "id": "y_0",
      "name": "かがり",
      "type": "yojo",
      "fruit": "strawberry",
      "description": "",
      "imageUrl": "/images/yojo/kagari.webp",
      "cost": 1,
      "hp": 1,
      "attack": 1,
      "effect": "",
      "role": ""
    }
  ]
}
`

type fakeRepo struct {
	created []ghapp.PullRequestInput
}

func (f *fakeRepo) FileContent(_ context.Context, path string) ([]byte, error) {
	if path == "src/data/yojo.json" {
		return []byte(yojoFixture), nil
	}
	return []byte("{\n  \"sweet\": []\n}\n"), nil
}

func (f *fakeRepo) CreatePullRequest(_ context.Context, in ghapp.PullRequestInput) (ghapp.PullRequest, error) {
	f.created = append(f.created, in)
	return ghapp.PullRequest{Number: 100 + len(f.created), HTMLURL: "https://github.com/ieyoukan/PPLALE-web/pull/101"}, nil
}

func (f *fakeRepo) RawURL(path string) string {
	return "https://raw.githubusercontent.com/ieyoukan/PPLALE-web/main/" + path
}

type harness struct {
	server *Server
	store  *store.Memory
	repo   *fakeRepo
	secret []byte
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	mem := store.NewMemory()
	repo := &fakeRepo{}
	signer, err := auth.NewStateSigner([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatalf("NewStateSigner: %v", err)
	}
	secret := []byte("webhook-secret")

	deps := Deps{
		Store:         mem,
		Sessions:      &auth.Sessions{Store: mem},
		Discord:       &auth.Discord{ClientID: "cid", RedirectURI: "https://cms.example/auth/callback"},
		StateSigner:   signer,
		Publisher:     &publish.Publisher{Repo: repo},
		CardReader:    repo,
		WebhookSecret: secret,
		Logger:        slog.New(slog.DiscardHandler),
		SubmitLimit:   3,
	}
	return &harness{server: New(deps), store: mem, repo: repo, secret: secret}
}

// login creates a session and returns a request decorator that authenticates
// as that user.
func (h *harness) login(t *testing.T, discordID, name string, role store.Role) func(*http.Request) {
	t.Helper()
	if role != "" {
		if err := h.store.UpsertUser(context.Background(), store.User{DiscordID: discordID, DisplayName: name, Role: role}); err != nil {
			t.Fatalf("UpsertUser: %v", err)
		}
	}
	rec := httptest.NewRecorder()
	sessions := &auth.Sessions{Store: h.store}
	if _, err := sessions.Issue(context.Background(), rec, auth.DiscordUser{ID: discordID, Username: name}); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	var token, csrf string
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case auth.SessionCookie:
			token = c.Value
		case auth.CSRFCookie:
			csrf = c.Value
		}
	}
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: token})
		r.Header.Set(auth.CSRFHeader, csrf)
	}
}

func (h *harness) do(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, r)
	return rec
}

// createDraft drives POST /api/drafts and returns the decoded draft.
func (h *harness) createDraft(t *testing.T, authenticate func(*http.Request), payload map[string]any, img []byte) (api.Draft, *httptest.ResponseRecorder) {
	t.Helper()
	body, contentType := draftBody(t, payload, img)
	req := httptest.NewRequest(http.MethodPost, "/api/drafts", body)
	req.Header.Set("Content-Type", contentType)
	authenticate(req)
	rec := h.do(req)
	var d api.Draft
	if rec.Code == http.StatusCreated {
		decode(t, rec, &d)
	}
	return d, rec
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/api/me", "/api/cards?kind=yojo", "/api/submissions", "/api/users", "/api/drafts"} {
		rec := h.do(httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s = %d, want 401", path, rec.Code)
		}
	}
	for _, path := range []string{"/api/drafts", "/api/drafts/submit"} {
		rec := h.do(httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s = %d, want 401", path, rec.Code)
		}
	}
}

// Logging in with Discord proves identity only. Queuing a draft requires an
// entry on the allow list.
func TestLoggedInButNotOnAllowListCannotQueueADraft(t *testing.T) {
	h := newHarness(t)
	authenticate := h.login(t, "100000000000000001", "stranger", "")

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	authenticate(req)
	rec := h.do(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/me = %d", rec.Code)
	}
	var me api.Me
	decode(t, rec, &me)
	if me.CanSubmit || me.Role != "" {
		t.Errorf("me = %+v, want no privileges", me)
	}

	_, draftRec := h.createDraft(t, authenticate, map[string]any{
		"kind": "yojo", "name": "テスト", "fruit": "melon", "cost": 1, "hp": 1, "attack": 1, "imageSlug": "test",
	}, testPNG())
	if draftRec.Code != http.StatusForbidden {
		t.Errorf("POST /api/drafts = %d, want 403", draftRec.Code)
	}
}

// The browser cannot reach a file inside a GitHub repository just from the
// site-relative imageUrl PPLALE-web stores, so /api/cards must also hand back
// a fetchable URL for it.
func TestCardsIncludeAFetchableImageURL(t *testing.T) {
	h := newHarness(t)
	authenticate := h.login(t, "100000000000000020", "viewer", store.RoleCreator)

	req := httptest.NewRequest(http.MethodGet, "/api/cards?kind=yojo", nil)
	authenticate(req)
	rec := h.do(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/cards = %d: %s", rec.Code, rec.Body)
	}

	var resp api.CardsResponse
	decode(t, rec, &resp)
	if len(resp.Cards) != 1 {
		t.Fatalf("got %d cards, want 1", len(resp.Cards))
	}
	card := resp.Cards[0]
	if card.ImageURL != "/images/yojo/kagari.webp" {
		t.Errorf("ImageURL = %q", card.ImageURL)
	}
	want := "https://raw.githubusercontent.com/ieyoukan/PPLALE-web/main/public/images/yojo/kagari.webp"
	if card.ImageDisplayURL != want {
		t.Errorf("ImageDisplayURL = %q, want %q", card.ImageDisplayURL, want)
	}
}

func TestCreateDraftConvertsImageAndServesIt(t *testing.T) {
	h := newHarness(t)
	authenticate := h.login(t, "100000000000000002", "creator", store.RoleCreator)

	draft, rec := h.createDraft(t, authenticate, map[string]any{
		"kind": "yojo", "name": "あたらしい子", "fruit": "melon",
		"cost": 2, "hp": 3, "attack": 1, "effect": "効果テキスト", "imageSlug": "atarashii",
	}, testPNG())
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/drafts = %d: %s", rec.Code, rec.Body)
	}
	if draft.IsEdit || draft.Kind != "yojo" || draft.Name != "あたらしい子" {
		t.Errorf("draft = %+v", draft)
	}
	if !draft.HasNewImage || draft.ImageDisplayURL != "/api/drafts/1/image" {
		t.Errorf("draft image = %+v", draft)
	}

	// The list endpoint must show the same queued draft.
	listReq := httptest.NewRequest(http.MethodGet, "/api/drafts", nil)
	authenticate(listReq)
	listRec := h.do(listReq)
	var list api.DraftsResponse
	decode(t, listRec, &list)
	if len(list.Drafts) != 1 || list.Drafts[0].ID != draft.ID {
		t.Fatalf("drafts list = %+v", list)
	}

	imgReq := httptest.NewRequest(http.MethodGet, draft.ImageDisplayURL, nil)
	authenticate(imgReq)
	imgRec := h.do(imgReq)
	if imgRec.Code != http.StatusOK || imgRec.Header().Get("Content-Type") != "image/webp" {
		t.Errorf("GET %s = %d %q", draft.ImageDisplayURL, imgRec.Code, imgRec.Header().Get("Content-Type"))
	}
	if imgRec.Body.Len() == 0 {
		t.Error("draft image body is empty")
	}
}

// An edit draft with no new image must resolve to the card's current
// upstream image, not a broken/placeholder URL.
func TestCreateEditDraftWithoutNewImageShowsCurrentImage(t *testing.T) {
	h := newHarness(t)
	authenticate := h.login(t, "100000000000000021", "creator", store.RoleCreator)

	draft, rec := h.createDraft(t, authenticate, map[string]any{
		"kind": "yojo", "id": "y_0", "name": "かがり(改)", "fruit": "strawberry",
		"cost": 1, "hp": 1, "attack": 1,
	}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/drafts = %d: %s", rec.Code, rec.Body)
	}
	if !draft.IsEdit || draft.HasNewImage {
		t.Errorf("draft = %+v", draft)
	}
	want := "https://raw.githubusercontent.com/ieyoukan/PPLALE-web/main/public/images/yojo/kagari.webp"
	if draft.ImageDisplayURL != want {
		t.Errorf("ImageDisplayURL = %q, want %q", draft.ImageDisplayURL, want)
	}
}

func TestCreateDraftReportsFieldValidationErrors(t *testing.T) {
	h := newHarness(t)
	authenticate := h.login(t, "100000000000000003", "creator", store.RoleCreator)

	_, rec := h.createDraft(t, authenticate, map[string]any{
		"kind": "yojo", "name": "", "fruit": "banana", "cost": -1, "hp": 1, "attack": 1, "imageSlug": "ok",
	}, testPNG())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
	}
	var resp api.ErrorResponse
	decode(t, rec, &resp)
	for _, field := range []string{"name", "fruit", "cost"} {
		if _, ok := resp.Fields[field]; !ok {
			t.Errorf("no error reported for %q: %+v", field, resp.Fields)
		}
	}
}

func TestCreateDraftRequiresCSRFToken(t *testing.T) {
	h := newHarness(t)
	authenticate := h.login(t, "100000000000000004", "creator", store.RoleCreator)

	body, contentType := draftBody(t, map[string]any{
		"kind": "yojo", "name": "x", "fruit": "all", "cost": 1, "hp": 1, "attack": 1, "imageSlug": "x",
	}, testPNG())
	req := httptest.NewRequest(http.MethodPost, "/api/drafts", body)
	req.Header.Set("Content-Type", contentType)
	authenticate(req)
	req.Header.Del(auth.CSRFHeader)

	if rec := h.do(req); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 without a CSRF token", rec.Code)
	}
}

func TestDeleteDraftIsOwnerOnly(t *testing.T) {
	h := newHarness(t)
	owner := h.login(t, "100000000000000022", "owner", store.RoleCreator)
	other := h.login(t, "100000000000000023", "other", store.RoleCreator)
	admin := h.login(t, "100000000000000024", "admin", store.RoleAdmin)

	draft, rec := h.createDraft(t, owner, map[string]any{
		"kind": "yojo", "name": "非公開下書き", "fruit": "all", "cost": 1, "hp": 1, "attack": 1, "imageSlug": "secret",
	}, testPNG())
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/drafts = %d", rec.Code)
	}

	deletePath := "/api/drafts/" + itoa(draft.ID)
	forbidden := httptest.NewRequest(http.MethodDelete, deletePath, nil)
	other(forbidden)
	if rec := h.do(forbidden); rec.Code != http.StatusForbidden {
		t.Errorf("another user's DELETE = %d, want 403", rec.Code)
	}

	// An admin may still manage anyone's draft.
	asAdmin := httptest.NewRequest(http.MethodDelete, deletePath, nil)
	admin(asAdmin)
	if rec := h.do(asAdmin); rec.Code != http.StatusOK {
		t.Errorf("admin DELETE = %d, want 200", rec.Code)
	}
}

func TestSubmitDraftsOpensOneBatchedPullRequestAndClearsTheQueue(t *testing.T) {
	h := newHarness(t)
	authenticate := h.login(t, "100000000000000005", "creator", store.RoleCreator)

	first, rec := h.createDraft(t, authenticate, map[string]any{
		"kind": "yojo", "name": "一人目", "fruit": "melon", "cost": 2, "hp": 3, "attack": 1, "imageSlug": "hitorime",
	}, testPNG())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create first draft = %d: %s", rec.Code, rec.Body)
	}
	second, rec := h.createDraft(t, authenticate, map[string]any{
		"kind": "sweet", "name": "二人目", "fruit": "all", "cost": 1, "hp": 0, "attack": 0,
		"sweetType": "cake", "imageSlug": "futarime",
	}, testPNG())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second draft = %d: %s", rec.Code, rec.Body)
	}

	submitReq := httptest.NewRequest(http.MethodPost, "/api/drafts/submit", nil)
	authenticate(submitReq)
	submitRec := h.do(submitReq)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("POST /api/drafts/submit = %d: %s", submitRec.Code, submitRec.Body)
	}

	var result api.SubmitResult
	decode(t, submitRec, &result)
	if result.Submission == nil || len(result.Submission.Cards) != 2 {
		t.Fatalf("result = %+v", result)
	}

	if len(h.repo.created) != 1 {
		t.Fatalf("created %d pull requests, want exactly 1 for the whole batch", len(h.repo.created))
	}
	pr := h.repo.created[0]
	if !strings.Contains(pr.CommitMessage, "discord:100000000000000005") {
		t.Errorf("commit message = %q, want the submitter recorded", pr.CommitMessage)
	}
	if !strings.Contains(pr.Title, "2件") {
		t.Errorf("title = %q, want it to mention both cards", pr.Title)
	}

	// The queue must be empty again: both drafts were consumed by the batch.
	listReq := httptest.NewRequest(http.MethodGet, "/api/drafts", nil)
	authenticate(listReq)
	listRec := h.do(listReq)
	var list api.DraftsResponse
	decode(t, listRec, &list)
	if len(list.Drafts) != 0 {
		t.Errorf("drafts remaining after submit = %+v", list.Drafts)
	}

	audit, err := h.store.ListSubmissions(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSubmissions: %v", err)
	}
	if len(audit) != 1 || len(audit[0].Cards) != 2 || audit[0].Status != store.StatusOpen {
		t.Errorf("audit = %+v", audit)
	}
	_ = first
	_ = second
}

func TestSubmitDraftsWithExplicitIDsLeavesOthersQueued(t *testing.T) {
	h := newHarness(t)
	authenticate := h.login(t, "100000000000000025", "creator", store.RoleCreator)

	keep, rec := h.createDraft(t, authenticate, map[string]any{
		"kind": "yojo", "name": "残す方", "fruit": "all", "cost": 1, "hp": 1, "attack": 1, "imageSlug": "nokosu",
	}, testPNG())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft = %d", rec.Code)
	}
	send, rec := h.createDraft(t, authenticate, map[string]any{
		"kind": "yojo", "name": "送る方", "fruit": "all", "cost": 1, "hp": 1, "attack": 1, "imageSlug": "okuru",
	}, testPNG())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft = %d", rec.Code)
	}

	reqBody, err := json.Marshal(api.SubmitDraftsRequest{IDs: []int64{send.ID}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	submitReq := httptest.NewRequest(http.MethodPost, "/api/drafts/submit", bytes.NewReader(reqBody))
	authenticate(submitReq)
	submitRec := h.do(submitReq)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("POST /api/drafts/submit = %d: %s", submitRec.Code, submitRec.Body)
	}
	var result api.SubmitResult
	decode(t, submitRec, &result)
	if len(result.Submission.Cards) != 1 || result.Submission.Cards[0].CardName != "送る方" {
		t.Fatalf("submitted cards = %+v", result.Submission.Cards)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/drafts", nil)
	authenticate(listReq)
	listRec := h.do(listReq)
	var list api.DraftsResponse
	decode(t, listRec, &list)
	if len(list.Drafts) != 1 || list.Drafts[0].ID != keep.ID {
		t.Errorf("remaining drafts = %+v, want only %d", list.Drafts, keep.ID)
	}
}

func TestSubmitDraftsRejectsEmptyQueue(t *testing.T) {
	h := newHarness(t)
	authenticate := h.login(t, "100000000000000026", "creator", store.RoleCreator)

	req := httptest.NewRequest(http.MethodPost, "/api/drafts/submit", nil)
	authenticate(req)
	if rec := h.do(req); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an empty queue", rec.Code)
	}
	if len(h.repo.created) != 0 {
		t.Error("a pull request was opened for an empty queue")
	}
}

func TestSubmitDraftsRateLimitPerUser(t *testing.T) {
	h := newHarness(t) // SubmitLimit is 3
	authenticate := h.login(t, "100000000000000006", "creator", store.RoleCreator)

	if _, rec := h.createDraft(t, authenticate, map[string]any{
		"kind": "yojo", "name": "連投", "fruit": "all", "cost": 1, "hp": 1, "attack": 1, "imageSlug": "spam",
	}, testPNG()); rec.Code != http.StatusCreated {
		t.Fatalf("create draft = %d", rec.Code)
	}

	send := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/drafts/submit", nil)
		authenticate(req)
		return h.do(req).Code
	}

	// The queue only has one draft: the first call consumes it (201), the
	// next two find nothing to submit (400), but all three still count
	// against the rate limit.
	if code := send(); code != http.StatusCreated {
		t.Fatalf("submit 1 = %d", code)
	}
	if code := send(); code != http.StatusBadRequest {
		t.Fatalf("submit 2 = %d", code)
	}
	if code := send(); code != http.StatusBadRequest {
		t.Fatalf("submit 3 = %d", code)
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Errorf("submit 4 = %d, want 429", code)
	}
}

func TestAllowListAdministrationRequiresAdmin(t *testing.T) {
	h := newHarness(t)
	creator := h.login(t, "100000000000000007", "creator", store.RoleCreator)

	req := httptest.NewRequest(http.MethodPut, "/api/users/100000000000000008", strings.NewReader(`{"role":"admin"}`))
	creator(req)
	if rec := h.do(req); rec.Code != http.StatusForbidden {
		t.Errorf("creator PUT /api/users = %d, want 403", rec.Code)
	}

	admin := h.login(t, "100000000000000009", "admin", store.RoleAdmin)
	req = httptest.NewRequest(http.MethodPut, "/api/users/100000000000000008", strings.NewReader(`{"role":"creator","displayName":"new"}`))
	admin(req)
	if rec := h.do(req); rec.Code != http.StatusOK {
		t.Fatalf("admin PUT /api/users = %d", rec.Code)
	}

	added, err := h.store.GetUser(context.Background(), "100000000000000008")
	if err != nil || added.Role != store.RoleCreator || added.AddedBy != "100000000000000009" {
		t.Errorf("stored user = %+v, err = %v", added, err)
	}
}

func TestUpsertUserRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	admin := h.login(t, "100000000000000010", "admin", store.RoleAdmin)

	cases := map[string]struct {
		id   string
		body string
	}{
		"unknown role":    {"100000000000000011", `{"role":"superuser"}`},
		"non numeric id":  {"not-a-snowflake", `{"role":"creator"}`},
		"too short an id": {"12345", `{"role":"creator"}`},
	}
	for name, tc := range cases {
		req := httptest.NewRequest(http.MethodPut, "/api/users/"+tc.id, strings.NewReader(tc.body))
		admin(req)
		if rec := h.do(req); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, rec.Code)
		}
	}
}

// Removing someone must also invalidate the browser they already have open.
func TestDeleteUserRevokesTheirSessions(t *testing.T) {
	h := newHarness(t)
	victim := h.login(t, "100000000000000012", "creator", store.RoleCreator)
	admin := h.login(t, "100000000000000013", "admin", store.RoleAdmin)

	probe := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	victim(probe)
	if rec := h.do(probe); rec.Code != http.StatusOK {
		t.Fatalf("victim GET /api/me = %d before removal", rec.Code)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/users/100000000000000012", nil)
	admin(req)
	if rec := h.do(req); rec.Code != http.StatusOK {
		t.Fatalf("admin DELETE = %d", rec.Code)
	}

	probe = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	victim(probe)
	if rec := h.do(probe); rec.Code != http.StatusUnauthorized {
		t.Errorf("victim GET /api/me = %d after removal, want 401", rec.Code)
	}
}

func TestAdminCannotDeleteThemselves(t *testing.T) {
	h := newHarness(t)
	admin := h.login(t, "100000000000000014", "admin", store.RoleAdmin)
	req := httptest.NewRequest(http.MethodDelete, "/api/users/100000000000000014", nil)
	admin(req)
	if rec := h.do(req); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestLoginRedirectsToDiscordWithSignedState(t *testing.T) {
	h := newHarness(t)
	rec := h.do(httptest.NewRequest(http.MethodGet, "/auth/login?return_to=/cards", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "code_challenge_method=S256") || !strings.Contains(location, "scope=identify") {
		t.Errorf("Location = %q", location)
	}

	var stateCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.StateCookie {
			stateCookie = c
		}
	}
	if stateCookie == nil || !stateCookie.HttpOnly {
		t.Fatalf("state cookie = %+v, want an HttpOnly cookie", stateCookie)
	}
	// The PKCE verifier must not be readable from the redirect URL.
	if strings.Contains(location, stateCookie.Value) {
		t.Error("the signed state leaked into the redirect URL")
	}
}

func TestDevSkipAuthLogsInWithoutDiscord(t *testing.T) {
	mem := store.NewMemory()
	if err := mem.UpsertUser(context.Background(), store.User{DiscordID: "1", DisplayName: "dev", Role: store.RoleAdmin}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	deps := Deps{
		Store:       mem,
		Sessions:    &auth.Sessions{Store: mem},
		StateSigner: mustSigner(t),
		Logger:      slog.New(slog.DiscardHandler),
		DevSkipAuth: true,
		DevUser:     auth.DiscordUser{ID: "1", Username: "dev"},
	}
	server := New(deps)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login?return_to=/cards", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/cards" {
		t.Errorf("Location = %q, want the return_to path (no Discord redirect)", loc)
	}

	var token string
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookie {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("no session cookie was issued")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: token})
	meRec := httptest.NewRecorder()
	server.ServeHTTP(meRec, req)
	if meRec.Code != http.StatusOK {
		t.Fatalf("GET /api/me = %d", meRec.Code)
	}
	var me api.Me
	decode(t, meRec, &me)
	if me.DiscordID != "1" || !me.CanManageUsers {
		t.Errorf("me = %+v", me)
	}
}

func mustSigner(t *testing.T) *auth.StateSigner {
	t.Helper()
	signer, err := auth.NewStateSigner([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatalf("NewStateSigner: %v", err)
	}
	return signer
}

func TestCallbackRejectsForgedState(t *testing.T) {
	h := newHarness(t)

	rec := h.do(httptest.NewRequest(http.MethodGet, "/auth/callback?code=x&state=y", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("without a state cookie = %d, want 400", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=x&state=wrong", nil)
	req.AddCookie(&http.Cookie{Name: auth.StateCookie, Value: "forged.value"})
	if rec := h.do(req); rec.Code != http.StatusBadRequest {
		t.Errorf("with a forged state cookie = %d, want 400", rec.Code)
	}
}

func TestWebhookSignature(t *testing.T) {
	h := newHarness(t)
	if _, err := h.store.CreateSubmission(context.Background(), store.Submission{
		PRNumber: 101, Cards: []store.SubmissionCard{{Kind: "yojo", CardID: "y_1"}}, Status: store.StatusOpen,
	}); err != nil {
		t.Fatalf("CreateSubmission: %v", err)
	}

	body := []byte(`{"action":"closed","pull_request":{"number":101,"merged":true}}`)

	unsigned := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	unsigned.Header.Set("X-GitHub-Event", "pull_request")
	if rec := h.do(unsigned); rec.Code != http.StatusUnauthorized {
		t.Errorf("unsigned webhook = %d, want 401", rec.Code)
	}

	wrong := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	wrong.Header.Set("X-GitHub-Event", "pull_request")
	wrong.Header.Set("X-Hub-Signature-256", sign(t, []byte("other-secret"), body))
	if rec := h.do(wrong); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrongly signed webhook = %d, want 401", rec.Code)
	}

	list, _ := h.store.ListSubmissions(context.Background(), 10)
	if list[0].Status != store.StatusOpen {
		t.Fatalf("status changed on a rejected webhook: %v", list[0].Status)
	}

	valid := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	valid.Header.Set("X-GitHub-Event", "pull_request")
	valid.Header.Set("X-Hub-Signature-256", sign(t, h.secret, body))
	if rec := h.do(valid); rec.Code != http.StatusOK {
		t.Fatalf("signed webhook = %d", rec.Code)
	}

	list, _ = h.store.ListSubmissions(context.Background(), 10)
	if list[0].Status != store.StatusMerged {
		t.Errorf("status = %q, want merged", list[0].Status)
	}
}

func TestWebhookStatusMapping(t *testing.T) {
	merged := pullRequestEvent{Action: "closed"}
	merged.PullRequest.Merged = true
	if status, ok := statusFor(merged); !ok || status != store.StatusMerged {
		t.Errorf("merged = (%q, %v)", status, ok)
	}

	closed := pullRequestEvent{Action: "closed"}
	if status, ok := statusFor(closed); !ok || status != store.StatusClosed {
		t.Errorf("closed = (%q, %v)", status, ok)
	}

	for _, action := range []string{"opened", "synchronize", "labeled", "reopened"} {
		if _, ok := statusFor(pullRequestEvent{Action: action}); ok {
			t.Errorf("action %q should not change the audit status", action)
		}
	}
}

func TestSecurityHeadersAreAlwaysSet(t *testing.T) {
	h := newHarness(t)
	rec := h.do(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("CSP = %q", rec.Header().Get("Content-Security-Policy"))
	}
}

func draftBody(t *testing.T, payload map[string]any, image []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := w.WriteField("payload", string(raw)); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if image != nil {
		part, err := w.CreateFormFile("image", "card.png")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatalf("write image: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func testPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 100, 150))
	for y := 0; y < 150; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func sign(t *testing.T, secret, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
