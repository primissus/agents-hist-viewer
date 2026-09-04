package domain

import "context"

type TranscriptSource interface {
	Sessions(ctx context.Context) ([]Session, error)
	Messages(ctx context.Context, sessionID string, yield func(Message) error) error
}

type TranscriptFingerprint struct {
	Path string
	Hash string
}

type TranscriptFingerprintSource interface {
	TranscriptFingerprint(ctx context.Context, sessionID string) (TranscriptFingerprint, error)
}

type PromptLog interface {
	Prompts(ctx context.Context) ([]Prompt, error)
}

type PlanSource interface {
	PlanPaths(ctx context.Context) ([]string, error)
}

type SearchRepository interface {
	Init(ctx context.Context) error
	ReplaceSession(ctx context.Context, s Session, msgs []Message) error
	GetFileHash(ctx context.Context, path string) (hash string, ok bool, err error)
	SetFileHash(ctx context.Context, path, sessionID, hash string) error
	Search(ctx context.Context, query string, limit int) ([]SearchHit, error)
	RecentSessions(ctx context.Context, q RecentQuery) ([]SearchHit, error)
	SessionByID(ctx context.Context, id string) (SessionDetail, error)
	Close() error
}

// RecentQuery selects recent sessions for the home view.
type RecentQuery struct {
	Limit       int
	RecordKind  RecordKind // empty = all
	ProjectPath string     // empty = all
	Vendor      Vendor     // empty = all
}
