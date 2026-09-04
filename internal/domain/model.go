package domain

import "time"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type BlockKind string

const (
	KindText       BlockKind = "text"
	KindThinking   BlockKind = "thinking"
	KindToolUse    BlockKind = "tool_use"
	KindToolResult BlockKind = "tool_result"
	KindImage      BlockKind = "image"
)

type Source string

const (
	SourceTranscript Source = "transcript"
	SourceHistory    Source = "history"
	SourcePlan       Source = "plan"
)

// RecordKind distinguishes chat sessions from plan documents.
type RecordKind string

const (
	RecordChat RecordKind = "chat"
	RecordPlan RecordKind = "plan"
)

// Vendor identifies the product that produced a session.
type Vendor string

const (
	VendorClaude        Vendor = "claude"
	VendorClaudeDesktop Vendor = "claude-desktop"
	VendorCursor        Vendor = "cursor"
)

func VendorLabel(v Vendor) string {
	switch v {
	case VendorCursor:
		return "Cursor"
	case VendorClaudeDesktop:
		return "Claude Desktop"
	default:
		return "Claude Code"
	}
}

type Session struct {
	ID            string
	Title         string
	ProjectPath   string
	GitBranch     string
	StartedAt     time.Time
	EndedAt       time.Time
	MessageCount  int
	FilePath      string
	HasTranscript bool
	RecordKind    RecordKind
	Vendor        Vendor
}

type Message struct {
	UUID        string
	ParentUUID  string
	SessionID   string
	Role        Role
	Kind        BlockKind
	Text        string
	ToolName    string
	Timestamp   time.Time
	Sequence    int
	IsSidechain bool
	Source      Source
	ProjectPath string
	GitBranch   string
}

type Prompt struct {
	SessionID string
	Project   string
	Text      string
	Timestamp time.Time
	Seq       int
}

type Plan struct {
	ID          string
	Title       string
	ProjectPath string
	FilePath    string
	Content     string
	ModTime     time.Time
	Vendor      Vendor
}

type SearchHit struct {
	SessionID     string
	SessionTitle  string
	ProjectPath   string
	FilePath      string
	MessageUUID   string
	Role          Role
	Kind          BlockKind
	Timestamp     time.Time
	StartedAt     time.Time
	Snippet       string
	Score         float64
	HasTranscript bool
	RecordKind    RecordKind
	Vendor        Vendor
}

type SessionDetail struct {
	Session  Session
	Messages []Message
}
