package ghapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the public GitHub REST API root.
const DefaultBaseURL = "https://api.github.com"

// maxResponseBytes bounds how much of a response body is read; dataset files
// are tens of kilobytes, so anything far larger signals something is wrong.
const maxResponseBytes = 32 << 20

// API is a thin GitHub REST transport shared by the token source and the
// repository client.
type API struct {
	BaseURL string
	HTTP    *http.Client
}

// NewAPI builds an API client with a sane timeout.
func NewAPI(baseURL string) *API {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &API{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Error is a non 2xx response from GitHub.
type Error struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("github %s %s: %d %s", e.Method, e.Path, e.StatusCode, e.Message)
}

func (a *API) do(ctx context.Context, method, path, authorization string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("ghapp: リクエストの生成に失敗しました: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("ghapp: リクエストの生成に失敗しました: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "pplale-cms")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := a.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ghapp: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("ghapp: レスポンスの読み取りに失敗しました: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{StatusCode: resp.StatusCode, Method: method, Path: path, Message: errorMessage(payload)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("ghapp: レスポンスの解析に失敗しました: %w", err)
	}
	return nil
}

func errorMessage(payload []byte) string {
	var parsed struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil || parsed.Message == "" {
		return strings.TrimSpace(string(payload))
	}
	msg := parsed.Message
	for _, e := range parsed.Errors {
		if e.Message != "" {
			msg += ": " + e.Message
		}
	}
	return msg
}
