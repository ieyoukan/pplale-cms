package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ieyoukan/pplale-cms/internal/auth"
)

func TestRolePermissions(t *testing.T) {
	cases := map[Role][2]bool{
		RoleAdmin:      {true, true},
		RoleCreator:    {true, false},
		Role(""):       {false, false},
		Role("viewer"): {false, false},
	}
	for role, want := range cases {
		if got := role.CanSubmit(); got != want[0] {
			t.Errorf("%q.CanSubmit() = %v, want %v", role, got, want[0])
		}
		if got := role.CanManageUsers(); got != want[1] {
			t.Errorf("%q.CanManageUsers() = %v, want %v", role, got, want[1])
		}
	}
	if Role("viewer").Valid() {
		t.Error("an unknown role passed validation")
	}
}

func TestMemoryUserLifecycle(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	if _, err := m.GetUser(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUser(missing) = %v, want ErrNotFound", err)
	}

	if err := m.UpsertUser(ctx, User{DiscordID: "1", DisplayName: "a", Role: RoleCreator}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	first, _ := m.GetUser(ctx, "1")
	if first.CreatedAt.IsZero() {
		t.Error("CreatedAt was not stamped")
	}

	// A role change must not reset when the user was first allowed in.
	if err := m.UpsertUser(ctx, User{DiscordID: "1", DisplayName: "a", Role: RoleAdmin}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	second, _ := m.GetUser(ctx, "1")
	if second.Role != RoleAdmin {
		t.Errorf("role = %q, want admin", second.Role)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Error("CreatedAt changed on update")
	}

	if err := m.DeleteUser(ctx, "1"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := m.GetUser(ctx, "1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("user survived deletion: %v", err)
	}
}

func TestMemorySubmissions(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	for i := 0; i < 3; i++ {
		if _, err := m.CreateSubmission(ctx, Submission{PRNumber: 10 + i, CardID: "y_1", Status: StatusOpen}); err != nil {
			t.Fatalf("CreateSubmission: %v", err)
		}
	}

	list, err := m.ListSubmissions(ctx, 2)
	if err != nil {
		t.Fatalf("ListSubmissions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("limit ignored: %d rows", len(list))
	}
	// Newest first.
	if list[0].PRNumber != 12 || list[1].PRNumber != 11 {
		t.Errorf("order = %d, %d", list[0].PRNumber, list[1].PRNumber)
	}

	if err := m.UpdateSubmissionStatusByPR(ctx, 11, StatusMerged); err != nil {
		t.Fatalf("UpdateSubmissionStatusByPR: %v", err)
	}
	all, _ := m.ListSubmissions(ctx, 0)
	for _, s := range all {
		want := StatusOpen
		if s.PRNumber == 11 {
			want = StatusMerged
		}
		if s.Status != want {
			t.Errorf("pr %d status = %q, want %q", s.PRNumber, s.Status, want)
		}
	}

	// A pull request opened by hand has no audit row.
	if err := m.UpdateSubmissionStatusByPR(ctx, 999, StatusMerged); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestMemorySessions(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	expires := time.Now().Add(time.Hour)

	sessions := []auth.Session{
		{TokenHash: "a", DiscordID: "1", ExpiresAt: expires},
		{TokenHash: "b", DiscordID: "1", ExpiresAt: expires},
		{TokenHash: "c", DiscordID: "2", ExpiresAt: expires},
	}
	for _, s := range sessions {
		if err := m.SaveSession(ctx, s); err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
	}

	if got, err := m.FindSession(ctx, "b"); err != nil || got.DiscordID != "1" {
		t.Errorf("FindSession = (%+v, %v)", got, err)
	}
	if err := m.DeleteSessionsByDiscordID(ctx, "1"); err != nil {
		t.Fatalf("DeleteSessionsByDiscordID: %v", err)
	}
	for _, hash := range []string{"a", "b"} {
		if _, err := m.FindSession(ctx, hash); !errors.Is(err, ErrNotFound) {
			t.Errorf("session %q survived: %v", hash, err)
		}
	}
	if _, err := m.FindSession(ctx, "c"); err != nil {
		t.Errorf("unrelated session was removed: %v", err)
	}
}
