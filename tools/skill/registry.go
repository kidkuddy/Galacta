package skill

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Registry holds all known skills, from built-ins and user-defined sources.
type Registry struct {
	skills map[string]SkillDef
}

// NewRegistry creates a registry loaded with built-in skills, installed plugin skills,
// global user skills, global commands, and project-level skills.
// Load order (later overrides earlier):
//
//	built-ins → installed plugins → global skills → global commands → project skills
func NewRegistry(workingDir string) *Registry {
	r := &Registry{skills: make(map[string]SkillDef)}

	// 1. Built-ins
	for _, s := range builtinSkills {
		r.skills[s.Name] = s
	}

	home, _ := os.UserHomeDir()

	// 2. Installed plugin skills via ~/.claude/plugins/installed_plugins.json
	if home != "" {
		r.loadInstalledPluginSkills(filepath.Join(home, ".claude", "plugins"))
	}

	// 3. Global user skills: ~/.claude/skills/{name}/SKILL.md
	if home != "" {
		r.loadSkillDirs(filepath.Join(home, ".claude", "skills"))
	}

	// 4. Global commands: ~/.claude/commands/{name}.md (no frontmatter)
	if home != "" {
		r.loadCommandFiles(filepath.Join(home, ".claude", "commands"))
	}

	// 5. Project-level skills: {workingDir}/.claude/skills/*.md and {name}/SKILL.md
	r.loadUserSkills(workingDir)
	r.loadSkillDirs(filepath.Join(workingDir, ".claude", "skills"))

	return r
}

// Register adds or replaces a skill in the registry.
func (r *Registry) Register(skill SkillDef) {
	r.skills[skill.Name] = skill
}

// Get returns the skill with the given name, if it exists.
func (r *Registry) Get(name string) (SkillDef, bool) {
	s, ok := r.skills[name]
	return s, ok
}

// List returns all skill names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	return names
}

// SkillInfo holds exported skill metadata for API responses.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListInfo returns name and description for all registered skills.
func (r *Registry) ListInfo() []SkillInfo {
	infos := make([]SkillInfo, 0, len(r.skills))
	for _, s := range r.skills {
		infos = append(infos, SkillInfo{Name: s.Name, Description: s.Description})
	}
	return infos
}

// loadInstalledPluginSkills reads installed_plugins.json and loads skills
// from each plugin's installPath/skills/{name}/SKILL.md.
func (r *Registry) loadInstalledPluginSkills(pluginsDir string) {
	data, err := os.ReadFile(filepath.Join(pluginsDir, "installed_plugins.json"))
	if err != nil {
		return
	}

	var manifest struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return
	}

	for _, entries := range manifest.Plugins {
		for _, entry := range entries {
			if entry.InstallPath == "" {
				continue
			}
			r.loadSkillDirs(filepath.Join(entry.InstallPath, "skills"))
		}
	}
}

// loadSkillDirs scans {dir}/{name}/SKILL.md for subdirectory-based skills.
func (r *Registry) loadSkillDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "SKILL.md")
		skill, err := parseSkillFile(path)
		if err != nil {
			continue
		}
		r.skills[skill.Name] = skill
	}
}

// loadCommandFiles scans {dir}/*.md as flat command files.
// Format: first line is the description, rest is the prompt body. No frontmatter.
func (r *Registry) loadCommandFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		skill, err := parseCommandFile(path)
		if err != nil {
			continue
		}
		r.skills[skill.Name] = skill
	}
}

// parseCommandFile reads a flat command file where the first line is the
// description and the rest is the prompt body (no frontmatter).
func parseCommandFile(path string) (SkillDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillDef{}, err
	}

	name := strings.TrimSuffix(filepath.Base(path), ".md")
	content := string(data)

	var description, prompt string
	if idx := strings.Index(content, "\n"); idx >= 0 {
		description = strings.TrimSpace(content[:idx])
		prompt = strings.TrimSpace(content[idx+1:])
	} else {
		description = strings.TrimSpace(content)
	}

	return SkillDef{
		Name:        name,
		Description: description,
		Prompt:      prompt,
	}, nil
}

// loadUserSkills reads .md files from {workingDir}/.claude/skills/ and
// parses them as skill definitions. Format:
//
//	---
//	name: my-skill
//	description: Does something
//	---
//	Prompt body here...
func (r *Registry) loadUserSkills(workingDir string) {
	dir := filepath.Join(workingDir, ".claude", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		skill, err := parseSkillFile(path)
		if err != nil {
			continue
		}
		r.skills[skill.Name] = skill
	}
}

// parseSkillFile reads a skill markdown file with YAML-like frontmatter.
func parseSkillFile(path string) (SkillDef, error) {
	f, err := os.Open(path)
	if err != nil {
		return SkillDef{}, err
	}
	defer f.Close()

	var skill SkillDef
	scanner := bufio.NewScanner(f)
	inFrontmatter := false
	var bodyLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if !inFrontmatter && strings.TrimSpace(line) == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter && strings.TrimSpace(line) == "---" {
			inFrontmatter = false
			continue
		}

		if inFrontmatter {
			if strings.HasPrefix(line, "name:") {
				skill.Name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			} else if strings.HasPrefix(line, "description:") {
				skill.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			}
		} else {
			bodyLines = append(bodyLines, line)
		}
	}

	if skill.Name == "" {
		// Fall back to filename without extension
		skill.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}

	skill.Prompt = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	return skill, scanner.Err()
}
