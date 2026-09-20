// Package httpapi exposes the CMS over HTTP: Discord login, the card
// submission endpoint that opens pull requests, allow list administration and
// the GitHub webhook that closes the loop when a pull request is merged.
package httpapi

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/ieyoukan/pplale-cms/internal/api"
	"github.com/ieyoukan/pplale-cms/internal/auth"
	"github.com/ieyoukan/pplale-cms/internal/publish"
	"github.com/ieyoukan/pplale-cms/internal/store"
)

// CardReader reads the current dataset files from the upstream repository and
// builds browser-fetchable URLs for the images those files reference.
type CardReader interface {
	FileContent(ctx context.Context, path string) ([]byte, error)
	RawURL(path string) string
}

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	Store         store.Store
	Sessions      *auth.Sessions
	Discord       *auth.Discord
	StateSigner   *auth.StateSigner
	Publisher     *publish.Publisher
	CardReader    CardReader
	WebhookSecret []byte
	Logger        *slog.Logger
	// StaticFS serves the built SPA. Nil disables static serving, which is
	// what the Vite dev server setup uses.
	StaticFS fs.FS
	// SubmitLimit caps submissions per user per hour.
	SubmitLimit int

	// DevSkipAuth bypasses Discord OAuth entirely: GET /auth/login logs the
	// browser straight in as DevUser instead of redirecting to Discord. The
	// caller (cmd/server) only ever sets this when config.DevSkipAuth passed
	// its ALLOW_INSECURE_COOKIES-only check, so this field carries no
	// independent safety check of its own — treat it as already validated.
	DevSkipAuth bool
	DevUser     auth.DiscordUser
}

// Server wires the routes together.
type Server struct {
	deps    Deps
	limiter *rateLimiter
	mux     *http.ServeMux
}

// New builds the HTTP handler.
func New(deps Deps) *Server {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.SubmitLimit <= 0 {
		deps.SubmitLimit = 20
	}
	if deps.DevSkipAuth {
		deps.Logger.Warn("DEV_SKIP_AUTH is enabled: Discord login is bypassed, every browser is signed in as the dev user",
			"discord_id", deps.DevUser.ID)
	}

	s := &Server{
		deps:    deps,
		limiter: newRateLimiter(deps.SubmitLimit, time.Hour),
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	s.mux.HandleFunc("GET /auth/login", s.handleLogin)
	s.mux.HandleFunc("GET /auth/callback", s.handleCallback)
	s.mux.Handle("POST /auth/logout", s.authenticated(s.handleLogout))

	s.mux.Handle("GET /api/me", s.authenticated(s.handleMe))
	s.mux.Handle("GET /api/datasets", s.authenticated(s.handleDatasets))
	s.mux.Handle("GET /api/cards", s.authenticated(s.handleCards))
	s.mux.Handle("POST /api/submissions", s.requireSubmit(s.handleSubmit))
	s.mux.Handle("GET /api/submissions", s.authenticated(s.handleSubmissions))

	s.mux.Handle("GET /api/users", s.requireAdmin(s.handleListUsers))
	s.mux.Handle("PUT /api/users/{discordID}", s.requireAdmin(s.handleUpsertUser))
	s.mux.Handle("DELETE /api/users/{discordID}", s.requireAdmin(s.handleDeleteUser))

	s.mux.HandleFunc("POST /webhooks/github", s.handleGitHubWebhook)

	if s.deps.StaticFS != nil {
		s.mux.Handle("GET /", spaHandler(s.deps.StaticFS))
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w)
	s.mux.ServeHTTP(w, r)
}

// securityHeaders applies defaults that matter for a credentialed admin UI.
func securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data: https://cdn.discordapp.com https://raw.githubusercontent.com; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, api.ErrorResponse{Error: message})
}
