package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kidkuddy/galacta/db"
)

// Manager is a session-scoped team registry. It owns team config on disk,
// the MessageBus, and the shared task DB.
type Manager struct {
	dataDir string
	mu      sync.RWMutex
	teams   map[string]*ActiveTeam
}

// ActiveTeam holds the runtime state for a team.
type ActiveTeam struct {
	Config    *Team
	Bus       *MessageBus
	TaskStore *db.SessionDB
}

// NewManager creates a new team manager rooted at dataDir.
func NewManager(dataDir string) *Manager {
	return &Manager{
		dataDir: dataDir,
		teams:   make(map[string]*ActiveTeam),
	}
}

// Create creates a new team: directory, config.json, tasks.db, and message bus.
func (m *Manager) Create(name, description, agentType string) (*Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.teams[name]; exists {
		return nil, fmt.Errorf("team %q already exists", name)
	}

	teamDir := filepath.Join(m.dataDir, "teams", name)
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating team directory: %w", err)
	}

	cfg := &Team{
		Name:        name,
		Description: description,
		AgentType:   agentType,
		Members:     []TeamMember{},
		CreatedAt:   time.Now(),
	}

	if err := writeConfig(teamDir, cfg); err != nil {
		os.RemoveAll(teamDir)
		return nil, err
	}

	taskStore, err := db.OpenPath(filepath.Join(teamDir, "tasks.db"))
	if err != nil {
		os.RemoveAll(teamDir)
		return nil, fmt.Errorf("opening team task db: %w", err)
	}

	m.teams[name] = &ActiveTeam{
		Config:    cfg,
		Bus:       NewMessageBus(),
		TaskStore: taskStore,
	}

	return cfg, nil
}

// Delete removes a team. Fails if agents are still registered on the bus.
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	at, ok := m.teams[name]
	if !ok {
		return fmt.Errorf("team %q not found", name)
	}

	members := at.Bus.Members()
	if len(members) > 0 {
		return fmt.Errorf("team %q still has active members: %v", name, members)
	}

	at.TaskStore.Close()

	teamDir := filepath.Join(m.dataDir, "teams", name)
	if err := os.RemoveAll(teamDir); err != nil {
		return fmt.Errorf("removing team directory: %w", err)
	}

	delete(m.teams, name)
	return nil
}

// Get returns the active team by name.
func (m *Manager) Get(name string) (*ActiveTeam, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	at, ok := m.teams[name]
	return at, ok
}

// AddMember registers an agent on the team's bus and appends to config.
func (m *Manager) AddMember(teamName string, member TeamMember) (<-chan TeamMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	at, ok := m.teams[teamName]
	if !ok {
		return nil, fmt.Errorf("team %q not found", teamName)
	}

	inbox := at.Bus.Register(member.Name)
	at.Config.Members = append(at.Config.Members, member)

	teamDir := filepath.Join(m.dataDir, "teams", teamName)
	writeConfig(teamDir, at.Config) // best-effort persist

	return inbox, nil
}

// RemoveMember unregisters an agent from the team's bus and removes from config.
func (m *Manager) RemoveMember(teamName, memberName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	at, ok := m.teams[teamName]
	if !ok {
		return fmt.Errorf("team %q not found", teamName)
	}

	at.Bus.Unregister(memberName)

	// Remove from config
	filtered := at.Config.Members[:0]
	for _, mem := range at.Config.Members {
		if mem.Name != memberName {
			filtered = append(filtered, mem)
		}
	}
	at.Config.Members = filtered

	teamDir := filepath.Join(m.dataDir, "teams", teamName)
	writeConfig(teamDir, at.Config) // best-effort persist

	return nil
}

func writeConfig(teamDir string, cfg *Team) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling team config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(teamDir, "config.json"), data, 0o644); err != nil {
		return fmt.Errorf("writing team config: %w", err)
	}
	return nil
}
