package domain

import "time"

// Summary is a cached, model-generated recap of a session's condensed
// transcript, keyed by (SessionID, Model). SourceHash pins the summary to
// the exact condensed text it was generated from, so a re-index that
// changes message content invalidates the cache.
type Summary struct {
	SessionID  string
	Model      string
	SourceHash string
	Text       string
	CreatedAt  time.Time
}
