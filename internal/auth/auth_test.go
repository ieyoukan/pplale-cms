package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSessionStore struct {
	mu       sync.Mutex
	sessions map[string]Session
}

func newFakeStore() *fakeSessionStore {
	return &fakeSessionStore{sessions: map[string]Session{}}
}

func (f *fakeSessionStore) SaveSession(_ context.Context, s Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.TokenHash] = s
	return nil
}

func (f *fakeSessionStore) FindSession(_ context.Context, hash string) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[hash]
	if !ok {
		return Session{}, errors.New("not found")
	}
	return s, nil
}

func (f *fakeSessionStore) DeleteSession(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, hash)
	return nil
}

func (f *fakeSessionStore) DeleteSessionsByDiscordID(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for hash, s := range f.sessions {
		if s.DiscordID == id {
			delete(f.sessions, hash)
		}
	}
	return nil
}

func TestSessionIssueSetsHardenedCookies(t *testing.T) {
	store := newFakeStore()
	m := &Sessions{Store: store, Secure: true}
	rec := httptest.NewRecorder()

	session, err := m.Issue(context.Background(), rec, DiscordUser{ID: "1", Username: "creator", GlobalName: "Creator"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if session.DisplayName != "Creator" {
		t.Errorf("DisplayName = %q, want the global name", session.DisplayName)
	}

	cookies := cookieMap(rec.Result().Cookies())
	sc, ok := cookies[SessionCookie]
	if !ok {
		t.Fatal("no session cookie was set")
	}
	if !sc.HttpOnly || !sc.Secure || sc.SameSite != http.SameSiteLaxMode || sc.Path != "/" {
		t.Errorf("session cookie is not hardened: %+v", sc)
	}

	// The raw token must never be persisted: only its hash is stored.
	if _, stored := store.sessions[sc.Value]; stored {
		t.Error("the raw session token was used as the storage key")
	}
	if _, stored := store.sessions[HashToken(sc.Value)]; !stored {
		t.Error("the session was not stored under its token hash")
	}

	csrf, ok := cookies[CSRFCookie]
	if !ok {
		t.Fatal("no CSRF cookie was set")
	}
	if csrf.HttpOnly {
		t.Error("the CSRF cookie must be readable by the SPA")
	}
	if csrf.Value != session.CSRFToken {
		t.Error("the CSRF cookie does not match the stored token")
	}
	if csrf.Value == sc.Value {
		t.Error("the CSRF token reuses the session token")
	}
}

func TestSessionCurrent(t *testing.T) {
	m := &Sessions{Store: newFakeStore(), Secure: true}
	rec := httptest.NewRecorder()
	if _, err := m.Issue(context.Background(), rec, DiscordUser{ID: "42", Username: "u"}); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	token := cookieMap(rec.Result().Cookies())[SessionCookie].Value

	got, err := m.Current(context.Background(), requestWithCookie(SessionCookie, token))
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got.DiscordID != "42" {
		t.Errorf("DiscordID = %q", got.DiscordID)
	}

	for name, req := range map[string]*http.Request{
		"no cookie":     httptest.NewRequest(http.MethodGet, "/", nil),
		"empty cookie":  requestWithCookie(SessionCookie, ""),
		"forged cookie": requestWithCookie(SessionCookie, "not-a-real-token"),
	} {
		if _, err := m.Current(context.Background(), req); !errors.Is(err, ErrNoSession) {
			t.Errorf("%s: err = %v, want ErrNoSession", name, err)
		}
	}
}

func TestSessionExpiryIsEnforcedServerSide(t *testing.T) {
	store := newFakeStore()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	m := &Sessions{Store: store, TTL: time.Hour, now: func() time.Time { return now }}

	rec := httptest.NewRecorder()
	if _, err := m.Issue(context.Background(), rec, DiscordUser{ID: "1", Username: "u"}); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	token := cookieMap(rec.Result().Cookies())[SessionCookie].Value

	now = now.Add(2 * time.Hour)
	if _, err := m.Current(context.Background(), requestWithCookie(SessionCookie, token)); !errors.Is(err, ErrNoSession) {
		t.Fatalf("err = %v, want ErrNoSession after expiry", err)
	}
	if len(store.sessions) != 0 {
		t.Error("the expired session was left in the store")
	}
}

func TestRevoke(t *testing.T) {
	store := newFakeStore()
	m := &Sessions{Store: store}
	rec := httptest.NewRecorder()
	if _, err := m.Issue(context.Background(), rec, DiscordUser{ID: "1", Username: "u"}); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	token := cookieMap(rec.Result().Cookies())[SessionCookie].Value

	out := httptest.NewRecorder()
	if err := m.Revoke(context.Background(), out, requestWithCookie(SessionCookie, token)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if len(store.sessions) != 0 {
		t.Error("the session survived revocation")
	}
	for _, name := range []string{SessionCookie, CSRFCookie} {
		c, ok := cookieMap(out.Result().Cookies())[name]
		if !ok || c.Value != "" || c.MaxAge != -1 {
			t.Errorf("%s was not cleared: %+v", name, c)
		}
	}
}

// Removing someone from the allow list must cut off their open browsers too,
// not just block their next login.
func TestRevokeAllForDiscordID(t *testing.T) {
	store := newFakeStore()
	m := &Sessions{Store: store}
	for i := 0; i < 3; i++ {
		id := "kept"
		if i < 2 {
			id = "revoked"
		}
		if _, err := m.Issue(context.Background(), httptest.NewRecorder(), DiscordUser{ID: id, Username: "u"}); err != nil {
			t.Fatalf("Issue: %v", err)
		}
	}

	if err := m.RevokeAllFor(context.Background(), "revoked"); err != nil {
		t.Fatalf("RevokeAllFor: %v", err)
	}
	if len(store.sessions) != 1 {
		t.Fatalf("%d sessions left, want only the unrelated one", len(store.sessions))
	}
	for _, s := range store.sessions {
		if s.DiscordID != "kept" {
			t.Errorf("the wrong session survived: %+v", s)
		}
	}
}

func TestCheckCSRF(t *testing.T) {
	session := Session{CSRFToken: "correct-token"}

	safe := httptest.NewRequest(http.MethodGet, "/api/cards", nil)
	if err := CheckCSRF(session, safe); err != nil {
		t.Errorf("GET was rejected: %v", err)
	}

	post := func(header string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/cards", nil)
		if header != "" {
			r.Header.Set(CSRFHeader, header)
		}
		return r
	}
	if err := CheckCSRF(session, post("")); err == nil {
		t.Error("POST without a CSRF header was accepted")
	}
	if err := CheckCSRF(session, post("wrong-token!!")); err == nil {
		t.Error("POST with a wrong CSRF token was accepted")
	}
	if err := CheckCSRF(session, post("correct-token")); err != nil {
		t.Errorf("POST with the correct token was rejected: %v", err)
	}
}

func TestPKCEChallengeIsSHA256OfVerifier(t *testing.T) {
	pkce, err := NewPKCE()
	if err != nil {
		t.Fatalf("NewPKCE: %v", err)
	}
	sum := sha256.Sum256([]byte(pkce.Verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); pkce.Challenge != want {
		t.Errorf("challenge = %q, want the S256 hash", pkce.Challenge)
	}

	other, _ := NewPKCE()
	if other.Verifier == pkce.Verifier {
		t.Error("two logins produced the same verifier")
	}
}

func TestAuthorizeURL(t *testing.T) {
	d := &Discord{ClientID: "cid", RedirectURI: "https://cms.example/auth/callback"}
	pkce := PKCE{Verifier: "v", Challenge: "chal"}

	parsed, err := url.Parse(d.AuthorizeURL("state123", pkce))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := parsed.Query()
	want := map[string]string{
		"client_id":             "cid",
		"response_type":         "code",
		"scope":                 "identify",
		"state":                 "state123",
		"code_challenge":        "chal",
		"code_challenge_method": "S256",
		"redirect_uri":          "https://cms.example/auth/callback",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	// Broader scopes would hand the CMS data it has no reason to hold.
	if strings.Contains(q.Get("scope"), "guilds") || strings.Contains(q.Get("scope"), "email") {
		t.Errorf("scope = %q, want identify only", q.Get("scope"))
	}
}

func TestStateSignerRoundTrip(t *testing.T) {
	signer := mustSigner(t)
	pkce := PKCE{Verifier: "verifier-value", Challenge: "c"}

	cookie, state, err := signer.Issue(pkce, "/cards/new")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	opened, err := signer.Open(cookie, state)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.Verifier != "verifier-value" || opened.ReturnTo != "/cards/new" {
		t.Errorf("opened = %+v", opened)
	}
}

func TestStateSignerRejectsTamperingAndReplay(t *testing.T) {
	signer := mustSigner(t)
	cookie, state, err := signer.Issue(PKCE{Verifier: "v"}, "/")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	payload, signature, _ := strings.Cut(cookie, ".")

	cases := map[string][2]string{
		"tampered payload":      {payload[:len(payload)-2] + "AA", state},
		"tampered signature":    {payload + "." + signature[:len(signature)-2] + "AA", state},
		"missing separator":     {payload, state},
		"state does not match":  {cookie, "someone-elses-state"},
		"empty state parameter": {cookie, ""},
	}
	for name, tc := range cases {
		value := tc[0]
		if name == "tampered payload" {
			value = tc[0] + "." + signature
		}
		if _, err := signer.Open(value, tc[1]); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	// A state signed with a different key must not open.
	other, _ := NewStateSigner([]byte(strings.Repeat("b", 32)))
	if _, err := other.Open(cookie, state); err == nil {
		t.Error("a state signed with another key was accepted")
	}
}

func TestStateSignerRejectsExpired(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	signer := mustSigner(t)
	signer.now = func() time.Time { return now }

	cookie, state, err := signer.Issue(PKCE{Verifier: "v"}, "/")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	now = now.Add(LoginStateTTL + time.Minute)
	if _, err := signer.Open(cookie, state); err == nil {
		t.Error("an expired login state was accepted")
	}
}

func TestNewStateSignerRejectsWeakKey(t *testing.T) {
	if _, err := NewStateSigner([]byte("short")); err == nil {
		t.Error("a short signing key was accepted")
	}
}

func TestSafeReturnPath(t *testing.T) {
	for in, want := range map[string]string{
		"/cards":               "/cards",
		"/cards?kind=yojo":     "/cards?kind=yojo",
		"":                     "/",
		"https://evil.example": "/",
		"//evil.example":       "/",
		"/\\evil.example":      "/",
		"javascript:alert(1)":  "/",
	} {
		if got := SafeReturnPath(in); got != want {
			t.Errorf("SafeReturnPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiscordExchange(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			form = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"access_token":"at","token_type":"Bearer"}`))
		case "/api/users/@me":
			if got := r.Header.Get("Authorization"); got != "Bearer at" {
				t.Errorf("Authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"999","username":"creator","global_name":"Creator"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	d := &Discord{ClientID: "cid", ClientSecret: "secret", RedirectURI: "https://cms.example/cb", BaseURL: srv.URL, HTTP: srv.Client()}
	user, err := d.Exchange(context.Background(), "code123", "verifier123")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if user.ID != "999" || user.DisplayName() != "Creator" {
		t.Errorf("user = %+v", user)
	}
	if form.Get("code") != "code123" || form.Get("code_verifier") != "verifier123" {
		t.Errorf("token request form = %v", form)
	}
	if form.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q", form.Get("grant_type"))
	}
}

func TestDiscordExchangeFailures(t *testing.T) {
	d := &Discord{ClientID: "cid"}
	if _, err := d.Exchange(context.Background(), "", "v"); err == nil {
		t.Error("an empty authorization code was accepted")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	d = &Discord{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := d.Exchange(context.Background(), "code", "v"); err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("err = %v, want the discord error surfaced", err)
	}
}

func mustSigner(t *testing.T) *StateSigner {
	t.Helper()
	s, err := NewStateSigner([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatalf("NewStateSigner: %v", err)
	}
	return s
}

func requestWithCookie(name, value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: name, Value: value})
	return r
}

func cookieMap(cookies []*http.Cookie) map[string]*http.Cookie {
	out := map[string]*http.Cookie{}
	for _, c := range cookies {
		out[c.Name] = c
	}
	return out
}
