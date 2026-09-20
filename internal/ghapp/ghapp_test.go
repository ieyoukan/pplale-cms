package ghapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppJWTIsVerifiableAndShortLived(t *testing.T) {
	key := testKey(t)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	token, err := appJWT("1234", key, now)
	if err != nil {
		t.Fatalf("appJWT: %v", err)
	}

	header, claims, sig, err := jwtParts(token)
	if err != nil {
		t.Fatalf("jwtParts: %v", err)
	}

	digest := sha256.Sum256([]byte(header + "." + claims))
	rawSig, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], rawSig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}

	var head map[string]string
	decodeSegment(t, header, &head)
	if head["alg"] != "RS256" || head["typ"] != "JWT" {
		t.Errorf("header = %v, want RS256/JWT", head)
	}

	var body struct {
		IAT int64  `json:"iat"`
		EXP int64  `json:"exp"`
		ISS string `json:"iss"`
	}
	decodeSegment(t, claims, &body)
	if body.ISS != "1234" {
		t.Errorf("iss = %q, want 1234", body.ISS)
	}
	if body.IAT != now.Add(-60*time.Second).Unix() {
		t.Errorf("iat = %d, want it backdated by 60s for clock skew", body.IAT)
	}
	// GitHub rejects assertions valid for more than 10 minutes.
	if lifetime := body.EXP - body.IAT; lifetime > 600 {
		t.Errorf("token lifetime = %ds, want <= 600s", lifetime)
	}
}

func TestAppJWTRequiresConfiguration(t *testing.T) {
	key := testKey(t)
	if _, err := appJWT("", key, time.Now()); err == nil {
		t.Error("expected an error when the app ID is missing")
	}
	if _, err := appJWT("1", nil, time.Now()); err == nil {
		t.Error("expected an error when the private key is missing")
	}
}

func TestParsePrivateKey(t *testing.T) {
	key := testKey(t)

	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if _, err := ParsePrivateKey(pkcs1); err != nil {
		t.Errorf("PKCS#1: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if _, err := ParsePrivateKey(pkcs8); err != nil {
		t.Errorf("PKCS#8: %v", err)
	}

	if _, err := ParsePrivateKey([]byte("not a key")); err == nil {
		t.Error("expected an error for a non PEM input")
	}
}

func TestTokenSourceCachesAndRefreshes(t *testing.T) {
	var calls int32
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/42/access_tokens" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		n := atomic.AddInt32(&calls, 1)
		writeJSON(t, w, map[string]any{
			"token":      fmt.Sprintf("ghs_token_%d", n),
			"expires_at": "2026-09-20T13:00:00Z",
		})
	}))
	defer srv.Close()

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	ts := &TokenSource{
		AppID: "1", InstallationID: 42, PrivateKey: testKey(t),
		API: NewAPI(srv.URL), now: func() time.Time { return now },
	}

	first, err := ts.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if first != "ghs_token_1" {
		t.Fatalf("token = %q", first)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Errorf("Authorization = %q, want a Bearer app JWT", gotAuth)
	}

	if second, _ := ts.Token(context.Background()); second != first {
		t.Errorf("token was re-minted while still valid: %q then %q", first, second)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("minted %d times, want 1 while cached", got)
	}

	// Inside the refresh margin the cached token must be discarded.
	now = time.Date(2026, 9, 20, 12, 59, 0, 0, time.UTC)
	third, err := ts.Token(context.Background())
	if err != nil {
		t.Fatalf("Token after expiry: %v", err)
	}
	if third != "ghs_token_2" {
		t.Errorf("token = %q, want a refreshed token", third)
	}
}

func TestTokenSourceSurfacesAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(t, w, map[string]string{"message": "Bad credentials"})
	}))
	defer srv.Close()

	ts := &TokenSource{AppID: "1", InstallationID: 42, PrivateKey: testKey(t), API: NewAPI(srv.URL)}
	_, err := ts.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("err = %v, want the GitHub message surfaced", err)
	}
}

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func TestClientFileContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/ieyoukan/PPLALE-web/contents/src/data/yojo.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("ref"); got != "main" {
			t.Errorf("ref = %q, want main", got)
		}
		if got := r.Header.Get("Authorization"); got != "token ghs_x" {
			t.Errorf("Authorization = %q", got)
		}
		// GitHub wraps the base64 payload at 60 columns.
		encoded := base64.StdEncoding.EncodeToString([]byte(`{"yojo":[]}`))
		writeJSON(t, w, map[string]string{"encoding": "base64", "content": encoded[:8] + "\n" + encoded[8:]})
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	got, err := c.FileContent(context.Background(), "src/data/yojo.json")
	if err != nil {
		t.Fatalf("FileContent: %v", err)
	}
	if string(got) != `{"yojo":[]}` {
		t.Errorf("content = %q", got)
	}
}

func TestClientCreatePullRequest(t *testing.T) {
	var seen []string
	bodies := map[string]map[string]any{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		seen = append(seen, key)
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body for %s: %v", key, err)
			}
			bodies[key] = body
		}

		switch {
		case key == "GET /repos/ieyoukan/PPLALE-web/git/ref/heads/main":
			writeJSON(t, w, map[string]any{"object": map[string]string{"sha": "basecommit"}})
		case key == "GET /repos/ieyoukan/PPLALE-web/git/commits/basecommit":
			writeJSON(t, w, map[string]any{"tree": map[string]string{"sha": "basetree"}})
		case key == "POST /repos/ieyoukan/PPLALE-web/git/blobs":
			writeJSON(t, w, map[string]string{"sha": "blob1"})
		case key == "POST /repos/ieyoukan/PPLALE-web/git/trees":
			writeJSON(t, w, map[string]string{"sha": "newtree"})
		case key == "POST /repos/ieyoukan/PPLALE-web/git/commits":
			writeJSON(t, w, map[string]string{"sha": "newcommit"})
		case key == "POST /repos/ieyoukan/PPLALE-web/git/refs":
			writeJSON(t, w, map[string]string{"ref": "refs/heads/cms/yojo-y_42"})
		case key == "POST /repos/ieyoukan/PPLALE-web/pulls":
			writeJSON(t, w, map[string]any{"number": 7, "html_url": "https://github.com/ieyoukan/PPLALE-web/pull/7"})
		default:
			t.Errorf("unexpected request %s", key)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	pr, err := testClient(srv.URL).CreatePullRequest(context.Background(), PullRequestInput{
		Branch:        "cms/yojo-y_42",
		Title:         "feat: add y_42",
		Body:          "submitted by discord:1",
		CommitMessage: "feat: add y_42 (submitted by discord:1)",
		Files: []File{
			{Path: "src/data/yojo.json", Content: []byte("json")},
			{Path: "public/images/yojo/a.webp", Content: []byte{0x00, 0xff}},
		},
	})
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if pr.Number != 7 || !strings.HasSuffix(pr.HTMLURL, "/pull/7") {
		t.Errorf("pr = %+v", pr)
	}

	// One blob per file, then exactly one tree, commit, ref and pull request.
	want := []string{
		"GET /repos/ieyoukan/PPLALE-web/git/ref/heads/main",
		"GET /repos/ieyoukan/PPLALE-web/git/commits/basecommit",
		"POST /repos/ieyoukan/PPLALE-web/git/blobs",
		"POST /repos/ieyoukan/PPLALE-web/git/blobs",
		"POST /repos/ieyoukan/PPLALE-web/git/trees",
		"POST /repos/ieyoukan/PPLALE-web/git/commits",
		"POST /repos/ieyoukan/PPLALE-web/git/refs",
		"POST /repos/ieyoukan/PPLALE-web/pulls",
	}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("request sequence =\n%v\nwant\n%v", seen, want)
	}

	if got := bodies["POST /repos/ieyoukan/PPLALE-web/git/blobs"]["encoding"]; got != "base64" {
		t.Errorf("blob encoding = %v", got)
	}
	tree := bodies["POST /repos/ieyoukan/PPLALE-web/git/trees"]
	if tree["base_tree"] != "basetree" {
		t.Errorf("base_tree = %v, want the base commit tree", tree["base_tree"])
	}
	if entries, ok := tree["tree"].([]any); !ok || len(entries) != 2 {
		t.Errorf("tree entries = %v, want 2", tree["tree"])
	}
	commit := bodies["POST /repos/ieyoukan/PPLALE-web/git/commits"]
	if parents, ok := commit["parents"].([]any); !ok || len(parents) != 1 || parents[0] != "basecommit" {
		t.Errorf("commit parents = %v, want [basecommit]", commit["parents"])
	}
	if got := bodies["POST /repos/ieyoukan/PPLALE-web/git/refs"]["ref"]; got != "refs/heads/cms/yojo-y_42" {
		t.Errorf("ref = %v", got)
	}
	pull := bodies["POST /repos/ieyoukan/PPLALE-web/pulls"]
	if pull["base"] != "main" || pull["head"] != "cms/yojo-y_42" {
		t.Errorf("pull request head/base = %v/%v", pull["head"], pull["base"])
	}
}

// Writing straight to the base branch would bypass human review, which the
// data contract forbids.
func TestClientRefusesToCommitOnBaseBranch(t *testing.T) {
	c := testClient("http://127.0.0.1:0")
	for _, branch := range []string{"", "main"} {
		_, err := c.CreatePullRequest(context.Background(), PullRequestInput{
			Branch: branch,
			Files:  []File{{Path: "a", Content: []byte("b")}},
		})
		if err == nil {
			t.Errorf("branch %q was accepted", branch)
		}
	}
}

func TestClientRejectsEmptyChangeset(t *testing.T) {
	_, err := testClient("http://127.0.0.1:0").CreatePullRequest(context.Background(), PullRequestInput{Branch: "cms/x"})
	if err == nil {
		t.Fatal("expected an error for a pull request with no files")
	}
}

func TestClientReportsExistingBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/ieyoukan/PPLALE-web/git/ref/heads/main":
			writeJSON(t, w, map[string]any{"object": map[string]string{"sha": "basecommit"}})
		case "GET /repos/ieyoukan/PPLALE-web/git/commits/basecommit":
			writeJSON(t, w, map[string]any{"tree": map[string]string{"sha": "basetree"}})
		case "POST /repos/ieyoukan/PPLALE-web/git/blobs":
			writeJSON(t, w, map[string]string{"sha": "blob1"})
		case "POST /repos/ieyoukan/PPLALE-web/git/trees":
			writeJSON(t, w, map[string]string{"sha": "newtree"})
		case "POST /repos/ieyoukan/PPLALE-web/git/commits":
			writeJSON(t, w, map[string]string{"sha": "newcommit"})
		case "POST /repos/ieyoukan/PPLALE-web/git/refs":
			w.WriteHeader(http.StatusUnprocessableEntity)
			writeJSON(t, w, map[string]string{"message": "Reference already exists"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).CreatePullRequest(context.Background(), PullRequestInput{
		Branch: "cms/yojo-y_42", Files: []File{{Path: "a", Content: []byte("b")}},
	})
	if err == nil || !strings.Contains(err.Error(), "既に存在します") {
		t.Fatalf("err = %v, want an existing-branch error", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 422 {
		t.Errorf("err does not wrap the 422 API error: %v", err)
	}
}

func TestEncodePathEscapesJapaneseFilenames(t *testing.T) {
	got := encodePath("public/images/yojo/111イチゴかがり.webp")
	if strings.Contains(got, "イチゴ") {
		t.Errorf("encodePath left the path unescaped: %q", got)
	}
	if !strings.HasPrefix(got, "public/images/yojo/") {
		t.Errorf("encodePath escaped the separators: %q", got)
	}
}

func TestRawURL(t *testing.T) {
	c := testClient("http://127.0.0.1:0")

	got := c.RawURL("public/images/yojo/111イチゴかがり.webp")
	want := "https://raw.githubusercontent.com/ieyoukan/PPLALE-web/main/public/images/yojo/" +
		"111%E3%82%A4%E3%83%81%E3%82%B4%E3%81%8B%E3%81%8C%E3%82%8A.webp"
	if got != want {
		t.Errorf("RawURL = %q, want %q", got, want)
	}
	if parsed, err := url.Parse(got); err != nil || parsed.Scheme != "https" {
		t.Errorf("RawURL produced an unparseable URL: %q (%v)", got, err)
	}

	c.RawHost = "raw.example.internal"
	if got := c.RawURL("public/a.webp"); got != "https://raw.example.internal/ieyoukan/PPLALE-web/main/public/a.webp" {
		t.Errorf("RawURL with a custom host = %q", got)
	}
}

func testClient(baseURL string) *Client {
	return &Client{
		Owner: "ieyoukan", Repo: "PPLALE-web", BaseBranch: "main",
		API: NewAPI(baseURL), Tokens: staticToken("ghs_x"),
	}
}

var cachedKey *rsa.PrivateKey

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	if cachedKey == nil {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		cachedKey = key
	}
	return cachedKey
}

func decodeSegment(t *testing.T, segment string, out any) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatalf("decode segment: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("unmarshal segment: %v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("write response: %v", err)
	}
}
