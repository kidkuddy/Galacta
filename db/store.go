package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type SessionDB struct {
	db   *sql.DB
	path string
}

// Open opens (or creates) a per-session SQLite database at {dataDir}/sessions/{sessionID}.db.
// Runs migrations automatically.
func Open(dataDir, sessionID string) (*SessionDB, error) {
	dir := filepath.Join(dataDir, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating sessions dir: %w", err)
	}

	dbPath := filepath.Join(dir, sessionID+".db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening session database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging session database: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &SessionDB{db: db, path: dbPath}, nil
}

// SaveMessage inserts a message row.
func (s *SessionDB) SaveMessage(msg *MessageRow) error {
	_, err := s.db.Exec(
		`INSERT INTO messages (id, role, content, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, model, stop_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.Role, msg.Content,
		msg.InputTokens, msg.OutputTokens, msg.CacheReadTokens, msg.CacheWriteTokens,
		msg.Model, msg.StopReason,
	)
	if err != nil {
		return fmt.Errorf("inserting message: %w", err)
	}
	return nil
}

// ListMessages returns all messages ordered by created_at.
func (s *SessionDB) ListMessages() ([]MessageRow, error) {
	rows, err := s.db.Query(
		`SELECT id, role, content, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, model, stop_reason, created_at
		 FROM messages ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	var messages []MessageRow
	for rows.Next() {
		var m MessageRow
		if err := rows.Scan(&m.ID, &m.Role, &m.Content,
			&m.InputTokens, &m.OutputTokens, &m.CacheReadTokens, &m.CacheWriteTokens,
			&m.Model, &m.StopReason, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning message row: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// GetUsageTotals returns summed token counts across all messages.
func (s *SessionDB) GetUsageTotals() (*UsageTotals, error) {
	var u UsageTotals
	err := s.db.QueryRow(
		`SELECT COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_write_tokens),0),
		        COUNT(*)
		 FROM messages`,
	).Scan(&u.TotalInputTokens, &u.TotalOutputTokens, &u.TotalCacheReadTokens, &u.TotalCacheWriteTokens, &u.MessageCount)
	if err != nil {
		return nil, fmt.Errorf("querying usage totals: %w", err)
	}
	return &u, nil
}

// SetMeta stores a key-value pair in the metadata table (upsert).
func (s *SessionDB) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO metadata (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("upserting metadata: %w", err)
	}
	return nil
}

// GetMeta retrieves a metadata value by key.
func (s *SessionDB) GetMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("querying metadata: %w", err)
	}
	return value, nil
}

// Close closes the database connection.
func (s *SessionDB) Close() error {
	return s.db.Close()
}

// DeleteSessionDB removes the session's database file from disk.
func DeleteSessionDB(dataDir, sessionID string) error {
	dbPath := filepath.Join(dataDir, "sessions", sessionID+".db")
	// Remove WAL and SHM files too
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting session database: %w", err)
	}
	return nil
}

func runMigrations(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		filename TEXT PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM _migrations WHERE filename = ?", entry.Name()).Scan(&count)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", entry.Name(), err)
		}
		if count > 0 {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}

		_, err = db.Exec(string(content))
		if err != nil {
			return fmt.Errorf("executing migration %s: %w", entry.Name(), err)
		}

		_, err = db.Exec("INSERT INTO _migrations (filename) VALUES (?)", entry.Name())
		if err != nil {
			return fmt.Errorf("recording migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}
