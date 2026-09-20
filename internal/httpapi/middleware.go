package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/ieyoukan/pplale-cms/internal/auth"
	"github.com/ieyoukan/pplale-cms/internal/store"
)

type contextKey string

const (
	sessionKey contextKey = "session"
	userKey    contextKey = "user"
)

type handlerFunc func(w http.ResponseWriter, r *http.Request)

// authenticated requires a valid session and a matching CSRF token on unsafe
// methods. Being logged in is not authorisation: it only proves identity.
func (s *Server) authenticated(next handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.deps.Sessions.Current(r.Context(), r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "ログインが必要です")
			return
		}
		if err := auth.CheckCSRF(session, r); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, session)

		// The allow list entry is read fresh on every request so revoking a
		// role takes effect immediately rather than at the next login.
		if user, err := s.deps.Store.GetUser(ctx, session.DiscordID); err == nil {
			ctx = context.WithValue(ctx, userKey, user)
		} else if !errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "権限の確認に失敗しました")
			return
		}
		next(w, r.WithContext(ctx))
	})
}

func (s *Server) requireRole(check func(store.Role) bool, denied string, next handlerFunc) http.Handler {
	return s.authenticated(func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFrom(r.Context())
		if !ok || !check(user.Role) {
			writeError(w, http.StatusForbidden, denied)
			return
		}
		next(w, r)
	})
}

func (s *Server) requireSubmit(next handlerFunc) http.Handler {
	return s.requireRole(store.Role.CanSubmit,
		"カードを提出する権限がありません。管理者に許可リストへの追加を依頼してください", next)
}

func (s *Server) requireAdmin(next handlerFunc) http.Handler {
	return s.requireRole(store.Role.CanManageUsers, "管理者権限が必要です", next)
}

func sessionFrom(ctx context.Context) (auth.Session, bool) {
	s, ok := ctx.Value(sessionKey).(auth.Session)
	return s, ok
}

func userFrom(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(userKey).(store.User)
	return u, ok
}

// rateLimiter is a fixed window counter keyed by Discord user. It exists to
// stop a compromised or careless account from flooding PPLALE-web with pull
// requests, not to shape traffic precisely.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	now    func() time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string][]time.Time{}}
}

func (l *rateLimiter) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

// Allow records an attempt and reports whether it is within the limit.
func (l *rateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// spaHandler serves the built frontend, falling back to index.html so client
// side routes survive a reload.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(fsys, name); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
