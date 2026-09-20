package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// LoginStateTTL bounds how long a started login may sit unfinished.
const LoginStateTTL = 10 * time.Minute

// LoginState is the server side half of a login attempt. It travels in a
// short lived signed cookie, never in the URL, so an attacker cannot forge a
// callback for a login the victim's browser never started.
type LoginState struct {
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r,omitempty"`
	Expires  int64  `json:"e"`
}

// StateSigner seals and opens login states with an HMAC key.
type StateSigner struct {
	Key []byte
	now func() time.Time
}

// NewStateSigner builds a signer. The key must be at least 32 bytes.
func NewStateSigner(key []byte) (*StateSigner, error) {
	if len(key) < 32 {
		return nil, errors.New("auth: 署名鍵は32バイト以上が必要です")
	}
	return &StateSigner{Key: key}, nil
}

// Issue starts a login attempt, returning the value to store in the cookie and
// the opaque state parameter handed to Discord.
func (s *StateSigner) Issue(pkce PKCE, returnTo string) (cookieValue, stateParam string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: state の生成に失敗しました: %w", err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(raw)

	state := LoginState{
		Nonce:    nonce,
		Verifier: pkce.Verifier,
		ReturnTo: returnTo,
		Expires:  s.clock().Add(LoginStateTTL).Unix(),
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return "", "", fmt.Errorf("auth: state のエンコードに失敗しました: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + s.sign(encoded), nonce, nil
}

// Open verifies a cookie value and checks it against the state parameter that
// came back from Discord.
func (s *StateSigner) Open(cookieValue, stateParam string) (LoginState, error) {
	encoded, signature, found := strings.Cut(cookieValue, ".")
	if !found {
		return LoginState{}, errors.New("auth: state cookie の形式が不正です")
	}
	if !hmac.Equal([]byte(signature), []byte(s.sign(encoded))) {
		return LoginState{}, errors.New("auth: state の署名が一致しません")
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return LoginState{}, fmt.Errorf("auth: state をデコードできません: %w", err)
	}
	var state LoginState
	if err := json.Unmarshal(payload, &state); err != nil {
		return LoginState{}, fmt.Errorf("auth: state を解析できません: %w", err)
	}
	if s.clock().Unix() > state.Expires {
		return LoginState{}, errors.New("auth: ログインの有効期限が切れました。もう一度お試しください")
	}
	if stateParam == "" || !hmac.Equal([]byte(state.Nonce), []byte(stateParam)) {
		return LoginState{}, errors.New("auth: state が一致しません")
	}
	return state, nil
}

func (s *StateSigner) sign(encoded string) string {
	mac := hmac.New(sha256.New, s.Key)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *StateSigner) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// SafeReturnPath rejects open redirects: only same-site absolute paths are
// allowed back from a login.
func SafeReturnPath(path string) string {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "\\") {
		return "/"
	}
	return path
}
