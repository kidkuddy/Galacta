package skill

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Registry holds all known skills, from built-ins and user-defined sources.
type Registry struct {
	skills map[string]SkillDef
}

// NewRegistry creates a registry loaded with built-in skills, plugin skills,
// and user-defined skills found in {workingDir}/.claude/skills/*.md.
// Load order (later overrides earlier): built-ins → global plugins → project plugins → user skills.
func NewRegistry(workingDir string) *Registry {
	r := &Registry{skills: make(map[string]SkillDef)}

	// Load built-ins first
	for _, s := range builtinSkills {
		r.skills[s.Name] = s
	}

	// Load global plugin skills: ~/.claude/plugins/*/skills/*.md
	if home, err := os.UserHomeDir(); err == nil {
		r.loadPluginSkills(filepath.Join(home, ".claude", "plugins"))
	}

	// Load project plugin skills: {workingDir}/.claude/plugins/*/skills/*.md
	r.loadPluginSkills(filepath.Join(workingDir, ".claude", "plugins"))

	// Load user skills last (highest priority)
	r.loadUserSkills(workingDir)

	return r
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

// loadPluginSkills scans {pluginsDir}/*/skills/*.md for skill files
// installed by plugins.
func (r *Registry) loadPluginSkills(pluginsDir string) {
	plugins, err := os.ReadDir(pluginsDir)
	if err != nil {
		return
	}

	for _, plugin := range plugins {
		if !plugin.IsDir() {
			continue
		}
		skillsDir := filepath.Join(pluginsDir, plugin.Name(), "skills")
		entries, err := os.ReadDir(skillsDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(skillsDir, entry.Name())
			skill, err := parseSkillFile(path)
			if err != nil {
				continue
			}
			r.skills[skill.Name] = skill
		}
	}
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
