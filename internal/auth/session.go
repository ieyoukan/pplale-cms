package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	// SessionCookie holds the opaque session id and is inaccessible to JS.
	SessionCookie = "pplale_cms_session"
	// CSRFCookie mirrors the session's CSRF token and is readable by the SPA
	// so it can echo the value back in a header (double submit).
	CSRFCookie = "pplale_cms_csrf"
	// CSRFHeader is the header the SPA must send on unsafe requests.
	CSRFHeader = "X-CSRF-Token"
	// StateCookie holds the signed login state during the OAuth round trip.
	StateCookie = "pplale_cms_oauth"

	// DefaultSessionTTL is how long a login lasts without re-authenticating.
	DefaultSessionTTL = 12 * time.Hour
)

// ErrNoSession means the request carried no usable session.
var ErrNoSession = errors.New("auth: セッションがありません")

// Session is a logged in browser. It is stored server side so an administrator
// can revoke access immediately; the cookie itself carries no claims.
type Session struct {
	// TokenHash is the SHA-256 of the cookie value. The raw token is never
	// persisted, so a dump of the session table cannot be replayed.
	TokenHash   string
	DiscordID   string
	DisplayName string
	CSRFToken   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// SessionStore persists sessions.
type SessionStore interface {
	SaveSession(ctx context.Context, s Session) error
	FindSession(ctx context.Context, tokenHash string) (Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteSessionsByDiscordID(ctx context.Context, discordID string) error
}

// Sessions issues, validates and revokes sessions.
type Sessions struct {
	Store SessionStore
	TTL   time.Duration
	// Secure marks cookies Secure. It is only ever false for local HTTP
	// development.
	Secure bool
	now    func() time.Time
}

func (m *Sessions) ttl() time.Duration {
	if m.TTL > 0 {
		return m.TTL
	}
	return DefaultSessionTTL
}

func (m *Sessions) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// Issue creates a session for a Discord identity and sets the cookies.
func (m *Sessions) Issue(ctx context.Context, w http.ResponseWriter, user DiscordUser) (Session, error) {
	token, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return Session{}, err
	}

	now := m.clock()
	s := Session{
		TokenHash:   HashToken(token),
		DiscordID:   user.ID,
		DisplayName: user.DisplayName(),
		CSRFToken:   csrf,
		CreatedAt:   now,
		ExpiresAt:   now.Add(m.ttl()),
	}
	if err := m.Store.SaveSession(ctx, s); err != nil {
		return Session{}, fmt.Errorf("auth: セッションの保存に失敗しました: %w", err)
	}

	http.SetCookie(w, m.cookie(SessionCookie, token, s.ExpiresAt, true))
	http.SetCookie(w, m.cookie(CSRFCookie, csrf, s.ExpiresAt, false))
	return s, nil
}

// Current resolves the session behind a request, rejecting expired ones.
func (m *Sessions) Current(ctx context.Context, r *http.Request) (Session, error) {
	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		return Session{}, ErrNoSession
	}
	s, err := m.Store.FindSession(ctx, HashToken(cookie.Value))
	if err != nil {
		return Session{}, ErrNoSession
	}
	if !m.clock().Before(s.ExpiresAt) {
		_ = m.Store.DeleteSession(ctx, s.TokenHash)
		return Session{}, ErrNoSession
	}
	return s, nil
}

// Revoke logs a browser out and clears its cookies.
func (m *Sessions) Revoke(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if cookie, err := r.Cookie(SessionCookie); err == nil && cookie.Value != "" {
		if err := m.Store.DeleteSession(ctx, HashToken(cookie.Value)); err != nil {
			return err
		}
	}
	http.SetCookie(w, m.cookie(SessionCookie, "", time.Unix(0, 0), true))
	http.SetCookie(w, m.cookie(CSRFCookie, "", time.Unix(0, 0), false))
	return nil
}

// RevokeAllFor drops every session of one Discord user, used when access is
// withdrawn from the allow list.
func (m *Sessions) RevokeAllFor(ctx context.Context, discordID string) error {
	return m.Store.DeleteSessionsByDiscordID(ctx, discordID)
}

// CheckCSRF enforces the double submit token on state changing requests.
func CheckCSRF(s Session, r *http.Request) error {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	sent := r.Header.Get(CSRFHeader)
	if sent == "" || !constantTimeEqual(sent, s.CSRFToken) {
		return errors.New("auth: CSRF トークンが一致しません")
	}
	return nil
}

// SetStateCookie stores the signed login state for the OAuth round trip.
func (m *Sessions) SetStateCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, m.cookie(StateCookie, value, m.clock().Add(LoginStateTTL), true))
}

// ClearStateCookie removes the login state once the callback is handled.
func (m *Sessions) ClearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, m.cookie(StateCookie, "", time.Unix(0, 0), true))
}

func (m *Sessions) cookie(name, value string, expires time.Time, httpOnly bool) *http.Cookie {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: httpOnly,
		Secure:   m.Secure,
		SameSite: http.SameSiteLaxMode,
	}
	if value == "" {
		c.MaxAge = -1
	}
	return c
}

// HashToken derives the stored form of a session token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: 乱数の生成に失敗しました: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
