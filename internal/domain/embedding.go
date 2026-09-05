package domain

import (
	"math"
	"time"
)

// Embedding is one stored vector for a message block under a given model.
type Embedding struct {
	MessageRowID int64
	SessionID    string
	Vector       []float32
	Model        string
}

// EmbedUnit is a message block selected as a candidate for embedding.
type EmbedUnit struct {
	MessageRowID int64
	SessionID    string
	Seq          int
	Role         Role
	Kind         BlockKind
	ToolName     string
	Text         string
}

// VectorHit is a nearest-neighbor search result.
type VectorHit struct {
	MessageRowID int64
	SessionID    string
	Score        float64
}

// EmbeddingContext is a stored vector joined with enough message/session
// metadata for pattern mining and RAG citations, without a second lookup.
type EmbeddingContext struct {
	MessageRowID int64
	SessionID    string
	ProjectPath  string
	Vendor       Vendor
	Timestamp    time.Time
	Text         string
	Vector       []float32
}

// Cosine returns the cosine similarity of two equal-length vectors.
func Cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
