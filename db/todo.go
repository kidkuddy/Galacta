package db

import (
	"fmt"
	"time"
)

// Todo represents a todo item in the session.
type Todo struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ReplaceTodos deletes all existing todos and inserts the provided list.
func (s *SessionDB) ReplaceTodos(todos []Todo) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM todos"); err != nil {
		return fmt.Errorf("deleting todos: %w", err)
	}

	now := time.Now()
	for _, t := range todos {
		if _, err := tx.Exec(
			"INSERT INTO todos (id, content, status, priority, updated_at) VALUES (?, ?, ?, ?, ?)",
			t.ID, t.Content, t.Status, t.Priority, now,
		); err != nil {
			return fmt.Errorf("inserting todo %s: %w", t.ID, err)
		}
	}

	return tx.Commit()
}

// ListTodos returns all todos ordered by rowid.
func (s *SessionDB) ListTodos() ([]Todo, error) {
	rows, err := s.db.Query("SELECT id, content, status, priority, updated_at FROM todos ORDER BY rowid")
	if err != nil {
		return nil, fmt.Errorf("querying todos: %w", err)
	}
	defer rows.Close()

	var todos []Todo
	for rows.Next() {
		var t Todo
		if err := rows.Scan(&t.ID, &t.Content, &t.Status, &t.Priority, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning todo row: %w", err)
		}
		todos = append(todos, t)
	}
	return todos, rows.Err()
}
