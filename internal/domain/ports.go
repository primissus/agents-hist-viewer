package domain

import (
	"context"
	"time"
)

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

// TranscriptCatalogEntry describes a discovered transcript without parsing it.
type TranscriptCatalogEntry struct {
	ID          string
	Vendor      Vendor
	Fingerprint TranscriptFingerprint
}

// TranscriptCatalogSource lists transcripts cheaply (directory walk + stat),
// so callers can skip unchanged sessions before reading any file content.
type TranscriptCatalogSource interface {
	TranscriptCatalog(ctx context.Context) ([]TranscriptCatalogEntry, error)
}

// TranscriptMetaSource resolves metadata for a single session without walking
// the whole source. Used with TranscriptCatalogSource for changed sessions.
type TranscriptMetaSource interface {
	SessionMeta(ctx context.Context, sessionID string) (Session, error)
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
	FileHashes(ctx context.Context) (map[string]string, error)
	SetFileHash(ctx context.Context, path, sessionID, hash string) error
	Search(ctx context.Context, query string, limit int, f SearchFilter) ([]SearchHit, error)
	RecentSessions(ctx context.Context, q RecentQuery) ([]SearchHit, error)
	SessionByID(ctx context.Context, id string) (SessionDetail, error)
	SessionsByIDs(ctx context.Context, ids []string) (map[string]Session, error)
	Close() error
}

// RecentQuery selects recent sessions for the home view.
type RecentQuery struct {
	Limit       int
	RecordKind  RecordKind // empty = all
	ProjectPath string     // empty = all
	Vendor      Vendor     // empty = all
	Since       time.Time  // zero = no filter
}

// SearchFilter narrows FTS search results.
type SearchFilter struct {
	Since time.Time // zero = no filter
}

// Embedder turns text into vectors using a local embedding model.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
	Dim() int
}

// ChatModel answers a question given a system and user prompt.
type ChatModel interface {
	Chat(ctx context.Context, system, user string) (string, error)
}

// EmbedFilter narrows embedding operations to a subset of messages.
type EmbedFilter struct {
	Vendor      Vendor
	ProjectPath string
	Roles       []Role
	Kinds       []BlockKind
	Since       time.Time
	Force       bool  // ignore already-embedded state (UnembeddedUnits only)
	AfterRowID  int64 // pagination cursor (UnembeddedUnits only): only rows with rowid > this
}

// EmbeddingRepository stores and queries message-block vectors for a model.
type EmbeddingRepository interface {
	InitEmbeddings(ctx context.Context) error
	UnembeddedUnits(ctx context.Context, model string, f EmbedFilter, limit int) ([]EmbedUnit, error)
	PutEmbeddings(ctx context.Context, e []Embedding) error
	Nearest(ctx context.Context, model string, q []float32, k int, f EmbedFilter) ([]VectorHit, error)
	EmbeddingsWithContext(ctx context.Context, model string, f EmbedFilter) ([]EmbeddingContext, error)
	MessageByRowID(ctx context.Context, rowid int64) (Message, error)
	Neighbors(ctx context.Context, sessionID string, seq, radius int) ([]Message, error)
	BashUnits(ctx context.Context, f EmbedFilter) ([]EmbedUnit, error)
}

// SummaryRepository caches per-session, per-model generated summaries.
type SummaryRepository interface {
	InitSummaries(ctx context.Context) error
	GetSummary(ctx context.Context, sessionID, model string) (Summary, bool, error)
	PutSummary(ctx context.Context, s Summary) error
}
