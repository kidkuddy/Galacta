CREATE TABLE IF NOT EXISTS todos (
    id         TEXT PRIMARY KEY,
    content    TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending',
    priority   TEXT NOT NULL DEFAULT 'medium',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
