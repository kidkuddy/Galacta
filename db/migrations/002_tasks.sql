CREATE TABLE IF NOT EXISTS tasks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    subject     TEXT NOT NULL,
    description TEXT DEFAULT '',
    active_form TEXT DEFAULT '',
    status      TEXT DEFAULT 'pending',
    owner       TEXT DEFAULT '',
    blocks      TEXT DEFAULT '[]',
    blocked_by  TEXT DEFAULT '[]',
    metadata    TEXT DEFAULT '{}',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
