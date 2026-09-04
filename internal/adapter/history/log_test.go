package history_test

import (
	"context"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/adapter/history"
)

const historyPath = "../../../testdata/history.jsonl"

func TestPastedContentMerged(t *testing.T) {
	log := history.NewLog(historyPath)
	prompts, err := log.Prompts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range prompts {
		if p.Text == "actual pasted content here" {
			return
		}
	}
	t.Error("expected pasted content to be merged into prompt text")
}

func TestSeqAssignedInTimestampOrder(t *testing.T) {
	log := history.NewLog(historyPath)
	prompts, err := log.Prompts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// sess-a1 has two prompts; first (ts=1000) should be Seq 0, second (ts=3000) Seq 1.
	seqs := map[int]string{}
	for _, p := range prompts {
		if p.SessionID == "sess-a1" {
			seqs[p.Seq] = p.Text
		}
	}
	if seqs[0] != "first prompt in session A" {
		t.Errorf("Seq 0 should be 'first prompt in session A', got %q", seqs[0])
	}
	if seqs[1] != "second prompt in session A" {
		t.Errorf("Seq 1 should be 'second prompt in session A', got %q", seqs[1])
	}
}

func TestTimestampFromEpochMs(t *testing.T) {
	log := history.NewLog(historyPath)
	prompts, err := log.Prompts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range prompts {
		if p.Text == "first prompt in session A" {
			want := time.UnixMilli(1700000001000).UTC()
			if !p.Timestamp.Equal(want) {
				t.Errorf("timestamp: got %v, want %v", p.Timestamp, want)
			}
			return
		}
	}
	t.Error("prompt not found")
}
