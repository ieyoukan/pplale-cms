// Package api defines every type that crosses the HTTP boundary. It is the
// single definition of the wire format: the Go handlers use these structs and
// the frontend's TypeScript types are generated from this file
// (`mise run gen-types`), so the two cannot drift apart.
package api

import "time"

// Role decides what an authenticated Discord user may do.
type Role string

const (
	// RoleCreator may submit cards, which opens a pull request for review.
	RoleCreator Role = "creator"
	// RoleAdmin may additionally manage the allow list.
	RoleAdmin Role = "admin"
)

// Valid reports whether a role is one the CMS knows.
func (r Role) Valid() bool { return r == RoleCreator || r == RoleAdmin }

// CanSubmit reports whether the role may open pull requests.
func (r Role) CanSubmit() bool { return r == RoleCreator || r == RoleAdmin }

// CanManageUsers reports whether the role may edit the allow list.
func (r Role) CanManageUsers() bool { return r == RoleAdmin }

// User is one entry on the allow list. Being logged in with Discord is not
// enough to submit; a user must also appear here.
type User struct {
	DiscordID   string    `json:"discordId"`
	DisplayName string    `json:"displayName"`
	Role        Role      `json:"role"`
	AddedBy     string    `json:"addedBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

// SubmissionStatus tracks a pull request through review.
type SubmissionStatus string

const (
	StatusOpen   SubmissionStatus = "pr_open"
	StatusMerged SubmissionStatus = "merged"
	StatusClosed SubmissionStatus = "closed"
)

// Submission is the audit record of one pull request opened by the CMS.
type Submission struct {
	ID          int64            `json:"id"`
	DiscordID   string           `json:"discordId"`
	DisplayName string           `json:"displayName"`
	Kind        string           `json:"kind"`
	CardID      string           `json:"cardId"`
	CardName    string           `json:"cardName"`
	Branch      string           `json:"branch"`
	PRNumber    int              `json:"prNumber"`
	PRURL       string           `json:"prUrl"`
	Status      SubmissionStatus `json:"status"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

// Me describes the signed in user to the frontend.
type Me struct {
	DiscordID      string `json:"discordId"`
	DisplayName    string `json:"displayName"`
	Role           Role   `json:"role"`
	CanSubmit      bool   `json:"canSubmit"`
	CanManageUsers bool   `json:"canManageUsers"`
}

// Card is one card as the editor sees it. Optional fields are null when the
// dataset does not carry them.
type Card struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Fruit       string  `json:"fruit"`
	Description string  `json:"description"`
	ImageURL    string  `json:"imageUrl"`
	Cost        int     `json:"cost"`
	HP          int     `json:"hp"`
	Attack      int     `json:"attack"`
	Effect      *string `json:"effect,omitempty"`
	Role        *string `json:"role,omitempty"`
	SweetType   *string `json:"sweetType,omitempty"`
	Version     *string `json:"version,omitempty"`
}

// CardsResponse is the live content of one dataset file on the base branch.
type CardsResponse struct {
	Kind   string `json:"kind"`
	NextID string `json:"nextId"`
	Cards  []Card `json:"cards"`
}

// Dataset describes one card file to the UI.
type Dataset struct {
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	IDPrefix string `json:"idPrefix"`
	CardType string `json:"cardType"`
	ImageDir string `json:"imageDir"`
}

// Metadata hands the form its option lists so the UI can never offer a value
// the backend would reject.
type Metadata struct {
	Datasets   []Dataset `json:"datasets"`
	Fruits     []string  `json:"fruits"`
	Roles      []string  `json:"roles"`
	SweetTypes []string  `json:"sweetTypes"`
	Versions   []string  `json:"versions"`
}

// SubmitPayload is the JSON part of the multipart submission. The image is
// sent alongside it as the `image` file field.
type SubmitPayload struct {
	Kind        string  `json:"kind"`
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Fruit       string  `json:"fruit"`
	Description string  `json:"description"`
	Cost        int     `json:"cost"`
	HP          int     `json:"hp"`
	Attack      int     `json:"attack"`
	Effect      *string `json:"effect,omitempty"`
	Role        *string `json:"role,omitempty"`
	SweetType   *string `json:"sweetType,omitempty"`
	Version     *string `json:"version,omitempty"`
	ImageSlug   string  `json:"imageSlug"`
}

// SubmitResult reports the pull request a submission opened.
type SubmitResult struct {
	CardID     string      `json:"cardId"`
	Branch     string      `json:"branch"`
	Files      []string    `json:"files"`
	PRURL      string      `json:"prUrl"`
	PRNumber   int         `json:"prNumber"`
	Submission *Submission `json:"submission"`
}

// UsersResponse is the allow list.
type UsersResponse struct {
	Users []User `json:"users"`
}

// SubmissionsResponse is the audit trail.
type SubmissionsResponse struct {
	Submissions []Submission `json:"submissions"`
}

// UpsertUserRequest adds or updates an allow list entry.
type UpsertUserRequest struct {
	DisplayName string `json:"displayName"`
	Role        Role   `json:"role"`
}

// StatusResponse is the acknowledgement returned by mutations with no payload.
type StatusResponse struct {
	Status string `json:"status"`
}

// ErrorResponse is the body of every non 2xx response. Fields carries
// per-input validation messages so the form can attach them to the right
// control.
type ErrorResponse struct {
	Error  string            `json:"error"`
	Fields map[string]string `json:"fields,omitempty"`
}
