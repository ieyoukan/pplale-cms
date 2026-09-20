// Package auth implements Discord OAuth2 login, server side sessions and the
// CSRF defence for pplale-cms. No GitHub credential is ever reachable from
// this layer: the browser only ever holds a session cookie.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DiscordAPIBase is the production Discord API root.
const DiscordAPIBase = "https://discord.com"

// DiscordUser is the authenticated identity, trimmed to what the CMS needs.
type DiscordUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
	Avatar     string `json:"avatar"`
}

// DisplayName prefers the modern display name and falls back to the handle.
func (u DiscordUser) DisplayName() string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	return u.Username
}

// Discord performs the OAuth2 authorization code flow with PKCE.
type Discord struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	BaseURL      string
	HTTP         *http.Client
}

func (d *Discord) baseURL() string {
	if d.BaseURL != "" {
		return strings.TrimSuffix(d.BaseURL, "/")
	}
	return DiscordAPIBase
}

func (d *Discord) client() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// PKCE is the proof key for one login attempt. The verifier stays server side
// (in the signed state cookie); only its hash travels to Discord.
type PKCE struct {
	Verifier  string
	Challenge string
}

// NewPKCE generates a fresh verifier and its S256 challenge.
func NewPKCE() (PKCE, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return PKCE{}, fmt.Errorf("auth: PKCE の生成に失敗しました: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return PKCE{Verifier: verifier, Challenge: base64.RawURLEncoding.EncodeToString(sum[:])}, nil
}

// AuthorizeURL builds the URL the browser is redirected to. Only the identify
// scope is requested: the CMS never needs to read a user's guilds or email.
func (d *Discord) AuthorizeURL(state string, pkce PKCE) string {
	q := url.Values{
		"client_id":             {d.ClientID},
		"redirect_uri":          {d.RedirectURI},
		"response_type":         {"code"},
		"scope":                 {"identify"},
		"state":                 {state},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
		"prompt":                {"none"},
	}
	return d.baseURL() + "/oauth2/authorize?" + q.Encode()
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

// Exchange trades the authorization code for an access token and immediately
// resolves the user behind it. The access token is deliberately not persisted:
// the CMS only needs the identity once, at login.
func (d *Discord) Exchange(ctx context.Context, code, verifier string) (DiscordUser, error) {
	if code == "" {
		return DiscordUser{}, errors.New("auth: 認可コードがありません")
	}

	form := url.Values{
		"client_id":     {d.ClientID},
		"client_secret": {d.ClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {d.RedirectURI},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.baseURL()+"/api/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return DiscordUser{}, fmt.Errorf("auth: リクエスト生成に失敗しました: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var token tokenResponse
	if err := d.send(req, &token); err != nil {
		return DiscordUser{}, fmt.Errorf("auth: トークン交換に失敗しました: %w", err)
	}
	if token.AccessToken == "" {
		return DiscordUser{}, errors.New("auth: アクセストークンが空です")
	}

	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL()+"/api/users/@me", nil)
	if err != nil {
		return DiscordUser{}, fmt.Errorf("auth: リクエスト生成に失敗しました: %w", err)
	}
	tokenType := token.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	userReq.Header.Set("Authorization", tokenType+" "+token.AccessToken)

	var user DiscordUser
	if err := d.send(userReq, &user); err != nil {
		return DiscordUser{}, fmt.Errorf("auth: ユーザー情報の取得に失敗しました: %w", err)
	}
	if user.ID == "" {
		return DiscordUser{}, errors.New("auth: Discord ユーザーIDを取得できませんでした")
	}
	return user, nil
}

func (d *Discord) send(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := d.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord %s: %d %s", req.URL.Path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}
