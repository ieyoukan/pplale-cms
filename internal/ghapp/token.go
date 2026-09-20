package ghapp

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// refreshMargin renews an installation token before it actually expires so a
// long running pull request never fails mid flight.
const refreshMargin = 2 * time.Minute

// TokenSource mints and caches installation access tokens for one GitHub App
// installation.
type TokenSource struct {
	AppID          string
	InstallationID int64
	PrivateKey     *rsa.PrivateKey
	API            *API

	now func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

type installationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Token returns a valid installation access token, minting a new one when the
// cached token is missing or close to expiry.
func (ts *TokenSource) Token(ctx context.Context) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	now := ts.clock()
	if ts.token != "" && now.Add(refreshMargin).Before(ts.expires) {
		return ts.token, nil
	}

	assertion, err := appJWT(ts.AppID, ts.PrivateKey, now)
	if err != nil {
		return "", err
	}

	var out installationToken
	path := fmt.Sprintf("/app/installations/%d/access_tokens", ts.InstallationID)
	err = ts.API.do(ctx, http.MethodPost, path, "Bearer "+assertion, nil, &out)
	if err != nil {
		return "", fmt.Errorf("ghapp: インストールトークンの取得に失敗しました: %w", err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("ghapp: インストールトークンが空でした")
	}

	ts.token = out.Token
	ts.expires = out.ExpiresAt
	return ts.token, nil
}

func (ts *TokenSource) clock() time.Time {
	if ts.now != nil {
		return ts.now()
	}
	return time.Now()
}
