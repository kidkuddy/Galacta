package systemprompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxMemoryLines = 200

// MemoryConfig holds configuration for memory prompt generation.
type MemoryConfig struct {
	// MemoryDir is the absolute path to the memory directory.
	// Typically ~/.galacta/projects/{project-hash}/memory/
	MemoryDir string
}

// BuildMemoryBlock returns the memory section to inject into the system prompt.
// Returns an empty string if memory is not configured or the directory doesn't exist.
func BuildMemoryBlock(cfg MemoryConfig) string {
	if cfg.MemoryDir == "" {
		return ""
	}

	// Ensure directory exists
	if err := os.MkdirAll(cfg.MemoryDir, 0755); err != nil {
		return ""
	}

	var b strings.Builder

	b.WriteString("# auto memory\n\n")
	b.WriteString(fmt.Sprintf("You have a persistent auto memory directory at `%s`. Contents survive across conversations. ", cfg.MemoryDir))
	b.WriteString("Use it to accumulate knowledge over time and become increasingly effective. ")
	b.WriteString("Building reliable context here is essential — it lets the user trust you with substantial, ongoing work.\n\n")

	b.WriteString("## You MUST access memories when:\n")
	b.WriteString("- Known memories appear relevant to the current task\n")
	b.WriteString("- The user references work from a prior conversation\n")
	b.WriteString("- The user explicitly asks you to check memory, recall, or remember\n\n")

	b.WriteString("## You MUST save memories when:\n")
	b.WriteString("- You encounter information (from the user or a tool) that would be useful in future conversations. ")
	b.WriteString("Ask yourself: \"Would this matter if I started fresh tomorrow?\" If yes, save or update before continuing.\n")
	b.WriteString("- The user describes goals, context, or direction (e.g., \"I'm building...\", \"we're migrating to...\", \"the goal is...\") — save for future reference.\n\n")

	b.WriteString("## Explicit user requests:\n")
	b.WriteString("- If asked to remember something, save it immediately. Signals: \"never...\", \"always...\", \"next time...\", \"remember...\"\n")
	b.WriteString("- If asked to forget something, find and remove the entry.\n")
	b.WriteString("- When the user corrects you on something you stated from memory, you MUST update or remove the incorrect entry.\n\n")

	b.WriteString("## What to save:\n")
	b.WriteString("- Reusable patterns and conventions not in CLAUDE.md\n")
	b.WriteString("- Project goals and context for future work\n")
	b.WriteString("- Architectural decisions, key file paths, project structure\n")
	b.WriteString("- User preferences — especially corrections or guidance during sessions\n")
	b.WriteString("- Solutions to likely-recurring problems; debugging insights\n")
	b.WriteString("- Anything explicitly requested to remember\n\n")

	b.WriteString("## What NOT to save:\n")
	b.WriteString("- Ephemeral task details — in-progress work, temporary state\n")
	b.WriteString("- Information duplicating or contradicting CLAUDE.md\n")
	b.WriteString("- Intra-session notes — conversation is auto-compressed, effectively unlimited; memory is for cross-session persistence\n\n")

	b.WriteString("## How to save memories:\n")
	b.WriteString("- Organize semantically by topic, not chronologically\n")
	b.WriteString("- Use the galacta_write and galacta_edit tools to update your memory files\n")
	b.WriteString("- `MEMORY.md` is always loaded into your conversation context — lines after 200 will be truncated, so keep it concise\n")
	b.WriteString("- Create separate topic files (e.g., `debugging.md`, `patterns.md`) for detailed notes and link to them from MEMORY.md\n")
	b.WriteString("- Update or remove memories that turn out to be wrong or outdated\n")
	b.WriteString("- Do not write duplicate memories. First check if there is an existing memory you can update before writing a new one.\n\n")

	// Load MEMORY.md content
	memoryMDPath := filepath.Join(cfg.MemoryDir, "MEMORY.md")
	content, err := os.ReadFile(memoryMDPath)
	if err != nil {
		b.WriteString("## MEMORY.md\n\n")
		b.WriteString("Your MEMORY.md is currently empty. When you save new memories, they will appear here.\n")
		return b.String()
	}

	memContent := strings.TrimSpace(string(content))
	if memContent == "" {
		b.WriteString("## MEMORY.md\n\n")
		b.WriteString("Your MEMORY.md is currently empty. When you save new memories, they will appear here.\n")
		return b.String()
	}

	// Truncate to maxMemoryLines
	lines := strings.Split(memContent, "\n")
	if len(lines) > maxMemoryLines {
		b.WriteString(fmt.Sprintf("WARNING: MEMORY.md is %d lines (limit: %d). Only the first %d lines were loaded. Move detailed content into separate topic files and keep MEMORY.md as a concise index.\n\n", len(lines), maxMemoryLines, maxMemoryLines))
		memContent = strings.Join(lines[:maxMemoryLines], "\n")
	}

	b.WriteString("## MEMORY.md\n\n")
	b.WriteString(memContent)
	b.WriteString("\n")

	return b.String()
}

// MemoryDirForProject returns the memory directory path for a given working directory.
// Uses a hash of the git root (or working dir if not a git repo) to create a
// project-specific memory directory under ~/.galacta/projects/.
func MemoryDirForProject(workingDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// Use git root if available, otherwise working dir
	root := findGitRoot(workingDir)
	if root == "" {
		root = workingDir
	}

	// Create a simple hash from the path
	// Use the path itself as the directory name (sanitized)
	sanitized := strings.ReplaceAll(root, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")
	sanitized = strings.TrimLeft(sanitized, "-")

	return filepath.Join(home, ".galacta", "projects", sanitized, "memory")
}
