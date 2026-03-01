package db

import (
	"encoding/json"
	"fmt"
	"time"
)

// Task represents a tracked task in the session.
type Task struct {
	ID          int            `json:"id"`
	Subject     string         `json:"subject"`
	Description string         `json:"description"`
	ActiveForm  string         `json:"activeForm,omitempty"`
	Status      string         `json:"status"`
	Owner       string         `json:"owner,omitempty"`
	Blocks      []int          `json:"blocks"`
	BlockedBy   []int          `json:"blockedBy"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// CreateTask inserts a new task with the given subject, description, and activeForm.
func (s *SessionDB) CreateTask(subject, description, activeForm string) (*Task, error) {
	now := time.Now()
	result, err := s.db.Exec(
		`INSERT INTO tasks (subject, description, active_form, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		subject, description, activeForm, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting task: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting task id: %w", err)
	}
	return &Task{
		ID:          int(id),
		Subject:     subject,
		Description: description,
		ActiveForm:  activeForm,
		Status:      "pending",
		Blocks:      []int{},
		BlockedBy:   []int{},
		Metadata:    map[string]any{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// GetTask retrieves a task by ID.
func (s *SessionDB) GetTask(id int) (*Task, error) {
	var t Task
	var blocksJSON, blockedByJSON, metadataJSON string
	err := s.db.QueryRow(
		`SELECT id, subject, description, active_form, status, owner, blocks, blocked_by, metadata, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.Subject, &t.Description, &t.ActiveForm, &t.Status, &t.Owner,
		&blocksJSON, &blockedByJSON, &metadataJSON, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("querying task %d: %w", id, err)
	}
	if err := json.Unmarshal([]byte(blocksJSON), &t.Blocks); err != nil {
		t.Blocks = []int{}
	}
	if err := json.Unmarshal([]byte(blockedByJSON), &t.BlockedBy); err != nil {
		t.BlockedBy = []int{}
	}
	if err := json.Unmarshal([]byte(metadataJSON), &t.Metadata); err != nil {
		t.Metadata = map[string]any{}
	}
	return &t, nil
}

// TaskUpdateOpts holds optional fields for updating a task.
type TaskUpdateOpts struct {
	Status      *string
	Subject     *string
	Description *string
	ActiveForm  *string
	Owner       *string
	AddBlocks   []int
	AddBlockedBy []int
	Metadata    map[string]any // merged into existing; nil values delete keys
}

// UpdateTask applies partial updates to a task.
func (s *SessionDB) UpdateTask(id int, opts TaskUpdateOpts) (*Task, error) {
	t, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}

	if opts.Status != nil {
		t.Status = *opts.Status
	}
	if opts.Subject != nil {
		t.Subject = *opts.Subject
	}
	if opts.Description != nil {
		t.Description = *opts.Description
	}
	if opts.ActiveForm != nil {
		t.ActiveForm = *opts.ActiveForm
	}
	if opts.Owner != nil {
		t.Owner = *opts.Owner
	}

	// Merge addBlocks
	if len(opts.AddBlocks) > 0 {
		existing := make(map[int]bool)
		for _, b := range t.Blocks {
			existing[b] = true
		}
		for _, b := range opts.AddBlocks {
			if !existing[b] {
				t.Blocks = append(t.Blocks, b)
			}
		}
	}

	// Merge addBlockedBy
	if len(opts.AddBlockedBy) > 0 {
		existing := make(map[int]bool)
		for _, b := range t.BlockedBy {
			existing[b] = true
		}
		for _, b := range opts.AddBlockedBy {
			if !existing[b] {
				t.BlockedBy = append(t.BlockedBy, b)
			}
		}
	}

	// Merge metadata
	if opts.Metadata != nil {
		if t.Metadata == nil {
			t.Metadata = map[string]any{}
		}
		for k, v := range opts.Metadata {
			if v == nil {
				delete(t.Metadata, k)
			} else {
				t.Metadata[k] = v
			}
		}
	}

	blocksJSON, _ := json.Marshal(t.Blocks)
	blockedByJSON, _ := json.Marshal(t.BlockedBy)
	metadataJSON, _ := json.Marshal(t.Metadata)
	now := time.Now()
	t.UpdatedAt = now

	_, err = s.db.Exec(
		`UPDATE tasks SET subject=?, description=?, active_form=?, status=?, owner=?,
		 blocks=?, blocked_by=?, metadata=?, updated_at=? WHERE id=?`,
		t.Subject, t.Description, t.ActiveForm, t.Status, t.Owner,
		string(blocksJSON), string(blockedByJSON), string(metadataJSON), now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("updating task %d: %w", id, err)
	}
	return t, nil
}

// ListTasks returns all non-deleted tasks.
func (s *SessionDB) ListTasks() ([]*Task, error) {
	rows, err := s.db.Query(
		`SELECT id, subject, description, active_form, status, owner, blocks, blocked_by, metadata, created_at, updated_at
		 FROM tasks WHERE status != 'deleted' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		var t Task
		var blocksJSON, blockedByJSON, metadataJSON string
		if err := rows.Scan(&t.ID, &t.Subject, &t.Description, &t.ActiveForm, &t.Status, &t.Owner,
			&blocksJSON, &blockedByJSON, &metadataJSON, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning task row: %w", err)
		}
		if err := json.Unmarshal([]byte(blocksJSON), &t.Blocks); err != nil {
			t.Blocks = []int{}
		}
		if err := json.Unmarshal([]byte(blockedByJSON), &t.BlockedBy); err != nil {
			t.BlockedBy = []int{}
		}
		if err := json.Unmarshal([]byte(metadataJSON), &t.Metadata); err != nil {
			t.Metadata = map[string]any{}
		}
		tasks = append(tasks, &t)
	}
	return tasks, rows.Err()
}
