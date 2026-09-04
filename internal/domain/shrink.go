package domain

// ShrinkConfig controls how block content is reduced before indexing.
type ShrinkConfig struct {
	Enabled        bool
	ToolPayloadCap int
	DropImages     bool
}

// DefaultShrinkConfig is the recommended indexing config.
var DefaultShrinkConfig = ShrinkConfig{
	Enabled:        true,
	ToolPayloadCap: 2000,
	DropImages:     true,
}

// Shrink reduces a block's raw content for indexing.
// Returns (text, true) to index, ("", false) to drop the block entirely.
// Images are always dropped when cfg.DropImages is true (even if cfg.Enabled is false).
// text and thinking blocks are kept verbatim.
// tool_use and tool_result are truncated rune-safe to ToolPayloadCap when cfg.Enabled is true.
func Shrink(kind BlockKind, raw string, cfg ShrinkConfig) (string, bool) {
	switch kind {
	case KindImage:
		if cfg.DropImages {
			return "", false
		}
		return raw, true
	case KindText, KindThinking:
		return raw, true
	case KindToolUse, KindToolResult:
		if cfg.Enabled && cfg.ToolPayloadCap > 0 {
			runes := []rune(raw)
			if len(runes) > cfg.ToolPayloadCap {
				return string(runes[:cfg.ToolPayloadCap]) + "…[truncated]", true
			}
		}
		return raw, true
	default:
		return raw, true
	}
}
