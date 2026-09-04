package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

var testCfg = ShrinkConfig{Enabled: true, ToolPayloadCap: 10, DropImages: true}

func TestShrink_ImageDropped(t *testing.T) {
	_, ok := Shrink(KindImage, "data:image/png;base64,abc==", testCfg)
	if ok {
		t.Fatal("image must be dropped (ok=false)")
	}
}

func TestShrink_TextKeptFull(t *testing.T) {
	raw := strings.Repeat("hello world ", 500)
	text, ok := Shrink(KindText, raw, testCfg)
	if !ok {
		t.Fatal("text must not be dropped")
	}
	if text != raw {
		t.Fatalf("text must be kept verbatim: got len %d want len %d", len(text), len(raw))
	}
}

func TestShrink_ThinkingKeptFull(t *testing.T) {
	raw := strings.Repeat("thinking deeply ", 300)
	text, ok := Shrink(KindThinking, raw, testCfg)
	if !ok {
		t.Fatal("thinking must not be dropped")
	}
	if text != raw {
		t.Fatalf("thinking must be kept verbatim: got len %d want len %d", len(text), len(raw))
	}
}

func TestShrink_ToolUseTruncated(t *testing.T) {
	raw := strings.Repeat("x", 100)
	text, ok := Shrink(KindToolUse, raw, testCfg)
	if !ok {
		t.Fatal("tool_use must not be dropped")
	}
	if !strings.HasSuffix(text, "…[truncated]") {
		t.Fatalf("tool_use must have truncation marker, got: %q", text)
	}
	if utf8.RuneCountInString(text) > testCfg.ToolPayloadCap+utf8.RuneCountInString("…[truncated]") {
		t.Fatalf("tool_use exceeds cap: %d runes", utf8.RuneCountInString(text))
	}
}

func TestShrink_ToolResultTruncated(t *testing.T) {
	raw := strings.Repeat("y", 100)
	text, ok := Shrink(KindToolResult, raw, testCfg)
	if !ok {
		t.Fatal("tool_result must not be dropped")
	}
	if !strings.HasSuffix(text, "…[truncated]") {
		t.Fatalf("tool_result must have truncation marker, got: %q", text)
	}
}

func TestShrink_ToolResultRuneSafe(t *testing.T) {
	// Build a string of multi-byte runes (e.g. Japanese chars = 3 bytes each).
	// Cap at 10 runes — truncation must not split a rune.
	raw := strings.Repeat("日", 50)
	text, ok := Shrink(KindToolResult, raw, testCfg)
	if !ok {
		t.Fatal("tool_result must not be dropped")
	}
	if !utf8.ValidString(text) {
		t.Fatal("result must be valid UTF-8 after truncation")
	}
	if !strings.HasSuffix(text, "…[truncated]") {
		t.Fatalf("expected truncation marker, got: %q", text)
	}
}

func TestShrink_IsErrorContentKept(t *testing.T) {
	// tool_result with is_error content must still be indexed (ok=true).
	raw := "error: command not found"
	_, ok := Shrink(KindToolResult, raw, testCfg)
	if !ok {
		t.Fatal("is_error tool_result must not be dropped")
	}
}

func TestShrink_BelowCapNotTruncated(t *testing.T) {
	raw := "short"
	text, ok := Shrink(KindToolUse, raw, testCfg)
	if !ok {
		t.Fatal("short tool_use must not be dropped")
	}
	if strings.Contains(text, "[truncated]") {
		t.Fatal("short content must not get truncation marker")
	}
	if text != raw {
		t.Fatalf("short content must be verbatim, got: %q", text)
	}
}

func TestShrink_DisabledNoTruncation(t *testing.T) {
	cfg := ShrinkConfig{Enabled: false, ToolPayloadCap: 5, DropImages: true}
	raw := strings.Repeat("z", 100)
	text, ok := Shrink(KindToolUse, raw, cfg)
	if !ok {
		t.Fatal("disabled shrink must not drop tool_use")
	}
	if text != raw {
		t.Fatal("disabled shrink must not truncate")
	}
}

func TestShrink_DisabledStillDropsImages(t *testing.T) {
	cfg := ShrinkConfig{Enabled: false, ToolPayloadCap: 0, DropImages: true}
	_, ok := Shrink(KindImage, "base64data", cfg)
	if ok {
		t.Fatal("DropImages=true must drop image even when Enabled=false")
	}
}
