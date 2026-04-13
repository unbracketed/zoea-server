package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

type SessionRecord struct {
	ID           string
	UserID       string
	ProjectID    string
	ExternalID   string
	Status       string
	PiPID        int
	CreatedAt    time.Time
	LastActiveAt time.Time
}

type ListSessionsQuery struct {
	UserID     string
	ExternalID string
	Limit      int
	Offset     int
}

type MessageRecord struct {
	SessionID string
	Role      string
	Content   string
	Model     string
	UsageJSON string
	Timestamp time.Time
}

type Store interface {
	Init(ctx context.Context) error
	Close() error

	CreateSession(ctx context.Context, s SessionRecord) error
	GetSession(ctx context.Context, id string) (SessionRecord, error)
	ListSessions(ctx context.Context, q ListSessionsQuery) ([]SessionRecord, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSessionActivity(ctx context.Context, id string, t time.Time) error

	ReplaceSessionMessages(ctx context.Context, sessionID string, msgs []MessageRecord) error
	CountSessions(ctx context.Context) (int, error)
	GetMaxSessionID(ctx context.Context) (string, error)
}

func Open(driver, dsn string) (Store, error) {
	switch driver {
	case "", "sqlite":
		return OpenSQLite(dsn)
	default:
		return nil, errors.New("unsupported store driver: " + driver)
	}
}
