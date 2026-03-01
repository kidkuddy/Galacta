package systemprompt

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeMDFile represents a discovered CLAUDE.md file.
type ClaudeMDFile struct {
	Path    string // absolute path
	Content string // file contents
	Source  string // "global", "project", "ancestor:{path}"
}

// DiscoverClaudeMD finds CLAUDE.md files in the following order:
// 1. ~/.claude/CLAUDE.md (global)
// 2. Walk upward from workingDir to /, collect CLAUDE.md at each level
// 3. {gitRoot}/.claude/CLAUDE.md (project-level, if in a git repo)
//
// Files are deduplicated by absolute path. Returned in order: global, ancestors (root→leaf), project.
func DiscoverClaudeMD(workingDir string) []ClaudeMDFile {
	seen := make(map[string]bool)
	var files []ClaudeMDFile

	// 1. Global: ~/.claude/CLAUDE.md
	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, ".claude", "CLAUDE.md")
		if f, err := readClaudeMD(globalPath, "global"); err == nil {
			seen[f.Path] = true
			files = append(files, f)
		}
	}

	// 2. Walk upward from workingDir to /
	ancestors := walkUpward(workingDir, seen)
	files = append(files, ancestors...)

	// 3. Project-level: {gitRoot}/.claude/CLAUDE.md
	if gitRoot := findGitRoot(workingDir); gitRoot != "" {
		projectPath := filepath.Join(gitRoot, ".claude", "CLAUDE.md")
		abs, _ := filepath.Abs(projectPath)
		if !seen[abs] {
			if f, err := readClaudeMD(projectPath, "project"); err == nil {
				seen[f.Path] = true
				files = append(files, f)
			}
		}

		// Also check CLAUDE.md at git root itself
		rootPath := filepath.Join(gitRoot, "CLAUDE.md")
		abs, _ = filepath.Abs(rootPath)
		if !seen[abs] {
			if f, err := readClaudeMD(rootPath, "project"); err == nil {
				seen[f.Path] = true
				files = append(files, f)
			}
		}
	}

	return files
}

func walkUpward(dir string, seen map[string]bool) []ClaudeMDFile {
	dir, _ = filepath.Abs(dir)

	// Collect paths from root to dir
	var paths []string
	cur := dir
	for {
		paths = append(paths, cur)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	// Reverse so we go root → leaf
	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}

	var files []ClaudeMDFile
	for _, p := range paths {
		claudePath := filepath.Join(p, "CLAUDE.md")
		abs, _ := filepath.Abs(claudePath)
		if seen[abs] {
			continue
		}
		if f, err := readClaudeMD(claudePath, "ancestor:"+p); err == nil {
			seen[f.Path] = true
			files = append(files, f)
		}
	}
	return files
}

func readClaudeMD(path, source string) (ClaudeMDFile, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ClaudeMDFile{}, err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return ClaudeMDFile{}, err
	}
	return ClaudeMDFile{
		Path:    abs,
		Content: strings.TrimSpace(string(content)),
		Source:  source,
	}, nil
}

func findGitRoot(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
