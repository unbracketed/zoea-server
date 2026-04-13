package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func testStore(t *testing.T) Store {
	t.Helper()
	s, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGetSession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	rec := SessionRecord{
		ID:           "s_000001",
		UserID:       "alice",
		ProjectID:    "proj1",
		ExternalID:   "telegram:123",
		Status:       "active",
		CreatedAt:    now,
		LastActiveAt: now,
	}
	if err := s.CreateSession(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetSession(ctx, "s_000001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "s_000001" {
		t.Fatalf("id: got %q", got.ID)
	}
	if got.UserID != "alice" {
		t.Fatalf("user_id: got %q", got.UserID)
	}
	if got.ProjectID != "proj1" {
		t.Fatalf("project_id: got %q", got.ProjectID)
	}
	if got.ExternalID != "telegram:123" {
		t.Fatalf("external_id: got %q", got.ExternalID)
	}
	if got.Status != "active" {
		t.Fatalf("status: got %q", got.Status)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetSession(context.Background(), "nope")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestExternalIDUniqueness(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	r1 := SessionRecord{ID: "s_000001", UserID: "a", ExternalID: "tg:1", Status: "active", CreatedAt: now, LastActiveAt: now}
	r2 := SessionRecord{ID: "s_000002", UserID: "b", ExternalID: "tg:1", Status: "active", CreatedAt: now, LastActiveAt: now}

	if err := s.CreateSession(ctx, r1); err != nil {
		t.Fatalf("create r1: %v", err)
	}
	err := s.CreateSession(ctx, r2)
	if err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestNullExternalIDAllowsDuplicates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	r1 := SessionRecord{ID: "s_000001", UserID: "a", Status: "active", CreatedAt: now, LastActiveAt: now}
	r2 := SessionRecord{ID: "s_000002", UserID: "b", Status: "active", CreatedAt: now, LastActiveAt: now}

	if err := s.CreateSession(ctx, r1); err != nil {
		t.Fatalf("create r1: %v", err)
	}
	if err := s.CreateSession(ctx, r2); err != nil {
		t.Fatalf("create r2: %v", err)
	}
}

func TestListSessions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i, uid := range []string{"alice", "alice", "bob"} {
		rec := SessionRecord{
			ID:           fmt.Sprintf("s_%06d", i+1),
			UserID:       uid,
			Status:       "active",
			CreatedAt:    now.Add(time.Duration(i) * time.Second),
			LastActiveAt: now,
		}
		if err := s.CreateSession(ctx, rec); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	// List all
	all, err := s.ListSessions(ctx, ListSessionsQuery{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}

	// Filter by user_id
	alice, err := s.ListSessions(ctx, ListSessionsQuery{UserID: "alice"})
	if err != nil {
		t.Fatalf("list alice: %v", err)
	}
	if len(alice) != 2 {
		t.Fatalf("expected 2, got %d", len(alice))
	}

	// Limit/offset
	page, err := s.ListSessions(ctx, ListSessionsQuery{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("expected 1, got %d", len(page))
	}
}

func TestListByExternalID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	r1 := SessionRecord{ID: "s_000001", UserID: "a", ExternalID: "tg:1", Status: "active", CreatedAt: now, LastActiveAt: now}
	r2 := SessionRecord{ID: "s_000002", UserID: "b", ExternalID: "tg:2", Status: "active", CreatedAt: now, LastActiveAt: now}
	s.CreateSession(ctx, r1)
	s.CreateSession(ctx, r2)

	results, err := s.ListSessions(ctx, ListSessionsQuery{ExternalID: "tg:1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "s_000001" {
		t.Fatalf("expected s_000001, got %q", results[0].ID)
	}
}

func TestDeleteSession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	rec := SessionRecord{ID: "s_000001", UserID: "a", Status: "active", CreatedAt: now, LastActiveAt: now}
	s.CreateSession(ctx, rec)

	if err := s.DeleteSession(ctx, "s_000001"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := s.GetSession(ctx, "s_000001")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	s := testStore(t)
	err := s.DeleteSession(context.Background(), "nope")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateSessionActivity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	rec := SessionRecord{ID: "s_000001", UserID: "a", Status: "active", CreatedAt: now, LastActiveAt: now}
	s.CreateSession(ctx, rec)

	later := now.Add(5 * time.Minute)
	if err := s.UpdateSessionActivity(ctx, "s_000001", later); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := s.GetSession(ctx, "s_000001")
	diff := got.LastActiveAt.Sub(later)
	if diff < -time.Second || diff > time.Second {
		t.Fatalf("last_active_at not updated: got %v, want ~%v", got.LastActiveAt, later)
	}
}

func TestReplaceSessionMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	rec := SessionRecord{ID: "s_000001", UserID: "a", Status: "active", CreatedAt: now, LastActiveAt: now}
	s.CreateSession(ctx, rec)

	msgs := []MessageRecord{
		{SessionID: "s_000001", Role: "user", Content: "Hello", Timestamp: now},
		{SessionID: "s_000001", Role: "assistant", Content: "Hi!", Timestamp: now.Add(time.Second)},
	}
	if err := s.ReplaceSessionMessages(ctx, "s_000001", msgs); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Replace with new set
	msgs2 := []MessageRecord{
		{SessionID: "s_000001", Role: "user", Content: "New", Timestamp: now},
	}
	if err := s.ReplaceSessionMessages(ctx, "s_000001", msgs2); err != nil {
		t.Fatalf("replace: %v", err)
	}
}

func TestGetMaxSessionID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Empty → ""
	id, err := s.GetMaxSessionID(ctx)
	if err != nil {
		t.Fatalf("max empty: %v", err)
	}
	if id != "" {
		t.Fatalf("expected empty, got %q", id)
	}

	// Insert some sessions
	for _, sid := range []string{"s_000001", "s_000005", "s_000003"} {
		s.CreateSession(ctx, SessionRecord{ID: sid, UserID: "a", Status: "active", CreatedAt: now, LastActiveAt: now})
	}

	id, err = s.GetMaxSessionID(ctx)
	if err != nil {
		t.Fatalf("max: %v", err)
	}
	if id != "s_000005" {
		t.Fatalf("expected s_000005, got %q", id)
	}
}

func TestDeleteCascadesMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	rec := SessionRecord{ID: "s_000001", UserID: "a", Status: "active", CreatedAt: now, LastActiveAt: now}
	s.CreateSession(ctx, rec)
	s.ReplaceSessionMessages(ctx, "s_000001", []MessageRecord{
		{SessionID: "s_000001", Role: "user", Content: "Hi", Timestamp: now},
	})

	if err := s.DeleteSession(ctx, "s_000001"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Messages should be gone (foreign key cascade)
	var count int
	s.(*SQLiteStore).db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_messages WHERE session_id = ?`, "s_000001").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 messages after cascade delete, got %d", count)
	}
}
