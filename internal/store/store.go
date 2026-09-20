// Package store holds the CMS side state: who may submit, which browsers are
// logged in, and an audit trail of every pull request opened upstream. The
// card data itself is never duplicated here; PPLALE-web remains the source of
// truth and is read back on every submission.
package store

import (
	"context"
	"errors"

	"github.com/ieyoukan/pplale-cms/internal/api"
	"github.com/ieyoukan/pplale-cms/internal/auth"
)

// ErrNotFound is returned when a lookup matches nothing.
var ErrNotFound = errors.New("store: 見つかりません")

// The persisted records are the same types the API returns; there is no second
// shape to keep in sync.
type (
	Role             = api.Role
	User             = api.User
	Submission       = api.Submission
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

// Store bundles everything the HTTP layer needs.
type Store interface {
	auth.SessionStore
	UserStore
	SubmissionStore
}
