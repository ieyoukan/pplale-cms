// Package ghapp authenticates as a GitHub App and performs the repository
// operations needed to open a pull request against PPLALE-web. Everything in
// this package runs server side: the installation token must never reach a
// browser.
package ghapp

import (
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
	"strings"
	"time"
)

// ParsePrivateKey reads the PEM private key downloaded from the GitHub App
// settings page. Both PKCS#1 and PKCS#8 encodings are accepted.
func ParsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("ghapp: PEM 形式の秘密鍵ではありません")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("ghapp: 秘密鍵を解析できません: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("ghapp: RSA 秘密鍵が必要です (got %T)", parsed)
	}
	return key, nil
}

// appJWT builds the short lived RS256 assertion GitHub accepts in exchange for
// an installation token. GitHub rejects tokens living longer than 10 minutes
// and clock skew in the past, hence the backdated iat.
func appJWT(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	if appID == "" {
		return "", errors.New("ghapp: App ID が未設定です")
	}
	if key == nil {
		return "", errors.New("ghapp: 秘密鍵が未設定です")
	}

	header, err := jsonSegment(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := jsonSegment(map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	})
	if err != nil {
		return "", err
	}

	signingInput := header + "." + claims
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("ghapp: JWT の署名に失敗しました: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func jsonSegment(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("ghapp: JWT セグメントの生成に失敗しました: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func jwtParts(token string) (header, claims, signature string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", "", errors.New("ghapp: JWT の形式が不正です")
	}
	return parts[0], parts[1], parts[2], nil
}
