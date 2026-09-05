package sqlite_test

import (
	"context"
	"testing"

	"claude-code-hist-viewer/internal/domain"
)

func unit(v float32, n int) []float32 {
	vec := make([]float32, n)
	for i := range vec {
		vec[i] = v
	}
	return vec
}

func TestEmbeddingRoundtripAndUnembedded(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s1", "embed session")
	msgs := []domain.Message{
		makeMsg("s1", "m1", 0, "hello world this is user text"),
		makeMsg("s1", "m2", 1, "short"), // will still show up as a candidate row
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}
	if err := r.InitEmbeddings(ctx); err != nil {
		t.Fatal(err)
	}

	units, err := r.UnembeddedUnits(ctx, "test-model", domain.EmbedFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 unembedded units, got %d: %+v", len(units), units)
	}

	if err := r.PutEmbeddings(ctx, []domain.Embedding{
		{MessageRowID: units[0].MessageRowID, SessionID: "s1", Vector: unit(1, 4), Model: "test-model"},
	}); err != nil {
		t.Fatal(err)
	}

	remaining, err := r.UnembeddedUnits(ctx, "test-model", domain.EmbedFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining unembedded unit, got %d", len(remaining))
	}

	forced, err := r.UnembeddedUnits(ctx, "test-model", domain.EmbedFilter{Force: true}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(forced) != 2 {
		t.Fatalf("expected 2 units with Force, got %d", len(forced))
	}

	msg, err := r.MessageByRowID(ctx, units[0].MessageRowID)
	if err != nil {
		t.Fatal(err)
	}
	if msg.UUID != "m1" {
		t.Fatalf("MessageByRowID got %q, want m1", msg.UUID)
	}
}

func TestNearestRanksByCosine(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s1", "nearest session")
	msgs := []domain.Message{
		makeMsg("s1", "close", 0, "close text"),
		makeMsg("s1", "far", 1, "far text"),
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}
	if err := r.InitEmbeddings(ctx); err != nil {
		t.Fatal(err)
	}

	units, err := r.UnembeddedUnits(ctx, "m", domain.EmbedFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	var closeRow, farRow int64
	for _, u := range units {
		switch {
		case u.Seq == 0:
			closeRow = u.MessageRowID
		case u.Seq == 1:
			farRow = u.MessageRowID
		}
	}
	if err := r.PutEmbeddings(ctx, []domain.Embedding{
		{MessageRowID: closeRow, SessionID: "s1", Vector: []float32{1, 0, 0, 0}, Model: "m"},
		{MessageRowID: farRow, SessionID: "s1", Vector: []float32{0, 1, 0, 0}, Model: "m"},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := r.Nearest(ctx, "m", []float32{1, 0, 0, 0}, 5, domain.EmbedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].MessageRowID != closeRow {
		t.Fatalf("expected closest hit first, got rowid=%d score=%f", hits[0].MessageRowID, hits[0].Score)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatalf("expected hits ranked by descending score, got %f then %f", hits[0].Score, hits[1].Score)
	}
}

func TestEmbeddingsSurviveReplaceSessionWhenTextUnchanged(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s1", "reattach session")
	msgs := []domain.Message{makeMsg("s1", "m1", 0, "stable text across reindex")}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}
	if err := r.InitEmbeddings(ctx); err != nil {
		t.Fatal(err)
	}

	units, err := r.UnembeddedUnits(ctx, "m", domain.EmbedFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}
	if err := r.PutEmbeddings(ctx, []domain.Embedding{
		{MessageRowID: units[0].MessageRowID, SessionID: "s1", Vector: []float32{1, 2, 3, 4}, Model: "m"},
	}); err != nil {
		t.Fatal(err)
	}

	// Re-index the same session with identical text but a fresh rowid.
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}

	remaining, err := r.UnembeddedUnits(ctx, "m", domain.EmbedFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected embedding to be reattached (0 unembedded), got %d", len(remaining))
	}

	items, err := r.EmbeddingsWithContext(ctx, "m", domain.EmbedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 embedding after reattach, got %d", len(items))
	}
	// PutEmbeddings normalizes on write, so compare direction, not magnitude.
	if got := domain.Cosine(items[0].Vector, []float32{1, 2, 3, 4}); got < 0.999 {
		t.Fatalf("reattached vector direction mismatch: cosine=%f vector=%v", got, items[0].Vector)
	}
}

func TestEmbeddingsDroppedWhenTextChanges(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s1", "changed text session")
	msgs := []domain.Message{makeMsg("s1", "m1", 0, "original text")}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}
	if err := r.InitEmbeddings(ctx); err != nil {
		t.Fatal(err)
	}
	units, err := r.UnembeddedUnits(ctx, "m", domain.EmbedFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.PutEmbeddings(ctx, []domain.Embedding{
		{MessageRowID: units[0].MessageRowID, SessionID: "s1", Vector: []float32{1, 2, 3, 4}, Model: "m"},
	}); err != nil {
		t.Fatal(err)
	}

	changed := []domain.Message{makeMsg("s1", "m1", 0, "completely different text now")}
	if err := r.ReplaceSession(ctx, s, changed); err != nil {
		t.Fatal(err)
	}

	remaining, err := r.UnembeddedUnits(ctx, "m", domain.EmbedFilter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected changed text to need re-embedding, got %d unembedded", len(remaining))
	}
}

func TestBashUnitsOnlyBashToolUse(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s1", "bash session")
	msgs := []domain.Message{
		{UUID: "b1", SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindToolUse, ToolName: "Bash",
			Text: `Bash {"command":"go test ./..."}`, Source: domain.SourceTranscript, Sequence: 0},
		{UUID: "b2", SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindToolUse, ToolName: "Read",
			Text: `Read {"file_path":"/tmp/x"}`, Source: domain.SourceTranscript, Sequence: 1},
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}

	units, err := r.BashUnits(ctx, domain.EmbedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || units[0].ToolName != "Bash" {
		t.Fatalf("expected 1 Bash unit, got %+v", units)
	}
}

func TestNeighborsReturnsWindowAroundSeq(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	s := makeSession("s1", "neighbors")
	msgs := []domain.Message{
		makeMsg("s1", "m0", 0, "zero"),
		makeMsg("s1", "m1", 1, "one"),
		makeMsg("s1", "m2", 2, "two"),
		makeMsg("s1", "m3", 3, "three"),
		makeMsg("s1", "m4", 4, "four"),
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}

	got, err := r.Neighbors(ctx, "s1", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].UUID != "m1" || got[2].UUID != "m3" {
		t.Fatalf("unexpected neighbors: %+v", got)
	}
}
