package sqlite

const ddl = `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    project_path TEXT NOT NULL DEFAULT '',
    git_branch TEXT NOT NULL DEFAULT '',
    started_at TEXT,
    ended_at TEXT,
    message_count INTEGER NOT NULL DEFAULT 0,
    file_path TEXT NOT NULL DEFAULT '',
    has_transcript INTEGER NOT NULL DEFAULT 0,
    record_kind TEXT NOT NULL DEFAULT 'chat',
    vendor TEXT NOT NULL DEFAULT 'claude'
);

CREATE TABLE IF NOT EXISTS messages (
    rowid INTEGER PRIMARY KEY,
    uuid TEXT NOT NULL,
    parent_uuid TEXT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    kind TEXT NOT NULL,
    tool_name TEXT,
    source TEXT NOT NULL DEFAULT 'transcript',
    timestamp TEXT,
    seq INTEGER NOT NULL,
    is_sidechain INTEGER NOT NULL DEFAULT 0,
    text TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    text, tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TABLE IF NOT EXISTS file_hashes (
    path TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
`
