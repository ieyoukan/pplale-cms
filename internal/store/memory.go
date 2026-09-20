package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ieyoukan/pplale-cms/internal/auth"
)

// Memory is an in-process Store used by tests and by local development runs
// that do not want a database.
type Memory struct {
	mu          sync.RWMutex
	sessions    map[string]auth.Session
	users       map[string]User
	submissions []Submission
	nextID      int64
	drafts      map[int64]Draft
	nextDraftID int64
	now         func() time.Time
}

// NewMemory builds an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		sessions:    map[string]auth.Session{},
		users:       map[string]User{},
		drafts:      map[int64]Draft{},
		nextID:      1,
		nextDraftID: 1,
	}
}

func (m *Memory) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *Memory) SaveSession(_ context.Context, s auth.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.TokenHash] = s
	return nil
}

func (m *Memory) FindSession(_ context.Context, tokenHash string) (auth.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[tokenHash]
	if !ok {
		return auth.Session{}, ErrNotFound
	}
	return s, nil
}

func (m *Memory) DeleteSession(_ context.Context, tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, tokenHash)
	return nil
}

func (m *Memory) DeleteSessionsByDiscordID(_ context.Context, discordID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for hash, s := range m.sessions {
		if s.DiscordID == discordID {
			delete(m.sessions, hash)
		}
	}
	return nil
}

func (m *Memory) GetUser(_ context.Context, discordID string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[discordID]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (m *Memory) ListUsers(_ context.Context) ([]User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DiscordID < out[j].DiscordID })
	return out, nil
}

func (m *Memory) UpsertUser(_ context.Context, u User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.users[u.DiscordID]; ok {
		u.CreatedAt = existing.CreatedAt
	} else if u.CreatedAt.IsZero() {
		u.CreatedAt = m.clock()
	}
	m.users[u.DiscordID] = u
	return nil
}

func (m *Memory) DeleteUser(_ context.Context, discordID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, discordID)
	return nil
}

func (m *Memory) CreateSubmission(_ context.Context, s Submission) (Submission, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	s.ID = m.nextID
	m.nextID++
	s.CreatedAt, s.UpdatedAt = now, now
	m.submissions = append(m.submissions, s)
	return s, nil
}

func (m *Memory) ListSubmissions(_ context.Context, limit int) ([]Submission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Submission, len(m.submissions))
	copy(out, m.submissions)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) UpdateSubmissionStatusByPR(_ context.Context, prNumber int, status SubmissionStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for i := range m.submissions {
		if m.submissions[i].PRNumber == prNumber {
			m.submissions[i].Status = status
			m.submissions[i].UpdatedAt = m.clock()
			found = true
		}
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

func (m *Memory) CreateDraft(_ context.Context, d Draft) (Draft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d.ID = m.nextDraftID
	m.nextDraftID++
	d.CreatedAt = m.clock()
	m.drafts[d.ID] = d
	return d, nil
}

func (m *Memory) ListDraftsByUser(_ context.Context, discordID string) ([]Draft, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Draft, 0, len(m.drafts))
	for _, d := range m.drafts {
		if d.DiscordID == discordID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) GetDraft(_ context.Context, id int64) (Draft, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.drafts[id]
	if !ok {
		return Draft{}, ErrNotFound
	}
	return d, nil
}

func (m *Memory) DeleteDrafts(_ context.Context, ids []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.drafts, id)
	}
	return nil
}
