package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ieyoukan/pplale-cms/internal/store"
)

// maxWebhookBytes bounds a webhook body; pull request events are a few KB.
const maxWebhookBytes = 1 << 20

// verifyWebhookSignature checks GitHub's HMAC over the raw body. Without this
// anyone could mark a submission merged.
func verifyWebhookSignature(secret, body []byte, header string) error {
	if len(secret) == 0 {
		return errors.New("webhook: 署名シークレットが未設定です")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(header), []byte(expected)) {
		return errors.New("webhook: 署名が一致しません")
	}
	return nil
}

type pullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Number int  `json:"number"`
		Merged bool `json:"merged"`
		Base   struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
}

// statusFor maps a pull_request event to the audit status it implies. The
// second return value reports whether the event is one we act on at all.
func statusFor(e pullRequestEvent) (store.SubmissionStatus, bool) {
	if e.Action != "closed" {
		return "", false
	}
	if e.PullRequest.Merged {
		return store.StatusMerged, true
	}
	return store.StatusClosed, true
}

func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "リクエストを読み取れませんでした")
		return
	}
	if err := verifyWebhookSignature(s.deps.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")); err != nil {
		s.deps.Logger.Warn("webhook rejected", "err", err)
		writeError(w, http.StatusUnauthorized, "署名の検証に失敗しました")
		return
	}
	if r.Header.Get("X-GitHub-Event") != "pull_request" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	var event pullRequestEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, "イベントを解析できませんでした")
		return
	}
	status, actionable := statusFor(event)
	if !actionable {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	number := event.PullRequest.Number
	if number == 0 {
		number = event.Number
	}
	if err := s.deps.Store.UpdateSubmissionStatusByPR(r.Context(), number, status); err != nil {
		// A pull request opened by hand has no audit row; that is not an error.
		s.deps.Logger.Debug("webhook status not applied", "pr", number, "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "pr": number, "state": status})
}
