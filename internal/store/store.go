// Package store holds the CMS side state: who may submit, which browsers are
// logged in, cards queued for submission but not yet sent, and an audit
// trail of every pull request opened upstream. The card data itself is never
// duplicated here; PPLALE-web remains the source of truth and is read back
// whenever a batch is published.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/ieyoukan/pplale-cms/internal/api"
	"github.com/ieyoukan/pplale-cms/internal/auth"
	"github.com/ieyoukan/pplale-cms/internal/cards"
)

// ErrNotFound is returned when a lookup matches nothing.
var ErrNotFound = errors.New("store: 見つかりません")

// The persisted audit records are the same types the API returns; there is no
// second shape to keep in sync.
type (
	Role             = api.Role
	User             = api.User
	Submission       = api.Submission
	SubmissionCard   = api.SubmissionCard
	SubmissionStatus = api.SubmissionStatus
)

const (
	RoleCreator = api.RoleCreator
	RoleAdmin   = api.RoleAdmin

	StatusOpen   = api.StatusOpen
	StatusMerged = api.StatusMerged
	StatusClosed = api.StatusClosed
)

// UserStore is the allow list.
type UserStore interface {
	GetUser(ctx context.Context, discordID string) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpsertUser(ctx context.Context, u User) error
	DeleteUser(ctx context.Context, discordID string) error
}

// SubmissionStore persists the audit trail.
type SubmissionStore interface {
	CreateSubmission(ctx context.Context, s Submission) (Submission, error)
	ListSubmissions(ctx context.Context, limit int) ([]Submission, error)
	UpdateSubmissionStatusByPR(ctx context.Context, prNumber int, status SubmissionStatus) error
}

// Draft is one card queued by a user but not yet sent to PPLALE-web. The
// image is converted to WebP/OGP-PNG at draft-creation time (imageconv is
// deterministic, so there is no reason to defer it and no reason to keep the
// raw upload around).
type Draft struct {
	ID          int64
	DiscordID   string
	DisplayName string
	Kind        string
	// CardID is set when this draft edits an existing card, empty for a new
	// card (its ID is allocated at publish time, against live data).
	CardID      string
	Name        string
	Fruit       string
	Description string
	Cost        int
	HP          int
	Attack      int
	Effect      *string
	Role        *string
	SweetType   *string
	Version     *string
	Taxonomy    cards.TaxonomyChanges
	ImageSlug   string
	// WebP and OGPPNG are nil when the draft edits a card and keeps its
	// existing image.
	WebP        []byte
	OGPPNG      []byte
	SourceBytes int
	SourceType  string
	CreatedAt   time.Time
}

// DraftStore persists queued-but-unsent cards.
type DraftStore interface {
	CreateDraft(ctx context.Context, d Draft) (Draft, error)
	ListDraftsByUser(ctx context.Context, discordID string) ([]Draft, error)
	GetDraft(ctx context.Context, id int64) (Draft, error)
	// DeleteDrafts removes drafts by ID, ignoring IDs that no longer exist.
	// Used both for a single manual removal and to clear a batch after it
	// has been published.
	DeleteDrafts(ctx context.Context, ids []int64) error
}

// Store bundles everything the HTTP layer needs.
type Store interface {
	auth.SessionStore
	UserStore
	SubmissionStore
	DraftStore
}
