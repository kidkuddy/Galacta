package fs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCP server with filesystem tools scoped to workingDir.
func NewServer(workingDir string) *server.MCPServer {
	srv := server.NewMCPServer(
		"galacta-fs",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerRead(srv, workingDir)
	registerWrite(srv, workingDir)
	registerEdit(srv, workingDir)
	registerGlob(srv, workingDir)
	registerGrep(srv, workingDir)

	return srv
}

// resolvePath resolves a file path relative to workingDir.
// Absolute paths are allowed as-is. Relative paths are joined with workingDir.
func resolvePath(workingDir, filePath string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}
	var resolved string
	if filepath.IsAbs(filePath) {
		resolved = filepath.Clean(filePath)
	} else {
		resolved = filepath.Clean(filepath.Join(workingDir, filePath))
	}
	return resolved, nil
}

// isBinary checks if data contains null bytes (indicating binary content).
func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func registerRead(srv *server.MCPServer, workingDir string) {
	tool := mcp.NewTool("galacta_read",
		mcp.WithDescription("Read file contents with line numbers"),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("File path (relative to working dir or absolute)")),
		mcp.WithNumber("offset", mcp.Description("Line offset to start reading from (1-based)")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of lines to read (default 2000)")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath := mcp.ParseString(req, "file_path", "")
		offset := mcp.ParseInt(req, "offset", 0)
		limit := mcp.ParseInt(req, "limit", 2000)

		resolved, err := resolvePath(workingDir, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		f, err := os.Open(resolved)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("cannot open file: %s", err.Error())), nil
		}
		defer f.Close()

		// Check for binary by reading first 512 bytes
		header := make([]byte, 512)
		n, err := f.Read(header)
		if err != nil && err != io.EOF {
			return mcp.NewToolResultError(fmt.Sprintf("reading file: %s", err.Error())), nil
		}
		if isBinary(header[:n]) {
			return mcp.NewToolResultError("file appears to be binary"), nil
		}

		// Seek back to start
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("seeking file: %s", err.Error())), nil
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line buffer

		var lines []string
		lineNum := 0
		collected := 0

		for scanner.Scan() {
			lineNum++
			if offset > 0 && lineNum < offset {
				continue
			}
			if collected >= limit {
				break
			}
			lines = append(lines, fmt.Sprintf("%6d\t%s", lineNum, scanner.Text()))
			collected++
		}

		if err := scanner.Err(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("scanning file: %s", err.Error())), nil
		}

		if len(lines) == 0 {
			return mcp.NewToolResultText("(empty file)"), nil
		}

		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	})
}

func registerWrite(srv *server.MCPServer, workingDir string) {
	tool := mcp.NewTool("galacta_write",
		mcp.WithDescription("Write content to a file, creating parent directories if needed"),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("File path (relative to working dir or absolute)")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content to write")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath := mcp.ParseString(req, "file_path", "")
		content := mcp.ParseString(req, "content", "")

		resolved, err := resolvePath(workingDir, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("creating directories: %s", err.Error())), nil
		}

		n := len([]byte(content))
		if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("writing file: %s", err.Error())), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Wrote %d bytes to %s", n, resolved)), nil
	})
}

func registerEdit(srv *server.MCPServer, workingDir string) {
	tool := mcp.NewTool("galacta_edit",
		mcp.WithDescription("Edit a file by replacing occurrences of old_string with new_string"),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("File path (relative to working dir or absolute)")),
		mcp.WithString("old_string", mcp.Required(), mcp.Description("Text to find and replace")),
		mcp.WithString("new_string", mcp.Required(), mcp.Description("Replacement text")),
		mcp.WithBoolean("replace_all", mcp.Description("Replace all occurrences (default false)")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath := mcp.ParseString(req, "file_path", "")
		oldStr := mcp.ParseString(req, "old_string", "")
		newStr := mcp.ParseString(req, "new_string", "")
		replaceAll := mcp.ParseBoolean(req, "replace_all", false)

		resolved, err := resolvePath(workingDir, filePath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		data, err := os.ReadFile(resolved)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("reading file: %s", err.Error())), nil
		}

		content := string(data)
		count := strings.Count(content, oldStr)

		if count == 0 {
			return mcp.NewToolResultError("old_string not found in file"), nil
		}

		if !replaceAll && count > 1 {
			return mcp.NewToolResultError(fmt.Sprintf("old_string is not unique, found %d occurrences. Provide more context to make it unique.", count)), nil
		}

		var result string
		if replaceAll {
			result = strings.ReplaceAll(content, oldStr, newStr)
		} else {
			result = strings.Replace(content, oldStr, newStr, 1)
		}

		if err := os.WriteFile(resolved, []byte(result), 0o644); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("writing file: %s", err.Error())), nil
		}

		if replaceAll {
			return mcp.NewToolResultText(fmt.Sprintf("Replaced %d occurrences in %s", count, resolved)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Replaced 1 occurrence in %s", resolved)), nil
	})
}

type fileEntry struct {
	path    string
	modTime time.Time
}

func registerGlob(srv *server.MCPServer, workingDir string) {
	tool := mcp.NewTool("galacta_glob",
		mcp.WithDescription("Find files matching a glob pattern, sorted by modification time (newest first)"),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Glob pattern (supports ** for recursive matching)")),
		mcp.WithString("path", mcp.Description("Base directory to search in (default: working directory)")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pattern := mcp.ParseString(req, "pattern", "")
		basePath := mcp.ParseString(req, "path", workingDir)

		if !filepath.IsAbs(basePath) {
			basePath = filepath.Join(workingDir, basePath)
		}
		basePath = filepath.Clean(basePath)

		var entries []fileEntry
		const maxResults = 200

		if strings.Contains(pattern, "**") {
			err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if len(entries) >= maxResults {
					return filepath.SkipAll
				}

				rel, err := filepath.Rel(basePath, path)
				if err != nil {
					return nil
				}

				if matchDoubleGlob(pattern, rel) {
					entries = append(entries, fileEntry{path: rel, modTime: info.ModTime()})
				}
				return nil
			})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("walking directory: %s", err.Error())), nil
			}
		} else {
			fullPattern := filepath.Join(basePath, pattern)
			matches, err := filepath.Glob(fullPattern)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid glob pattern: %s", err.Error())), nil
			}

			for _, m := range matches {
				if len(entries) >= maxResults {
					break
				}
				info, err := os.Stat(m)
				if err != nil {
					continue
				}
				rel, err := filepath.Rel(basePath, m)
				if err != nil {
					rel = m
				}
				entries = append(entries, fileEntry{path: rel, modTime: info.ModTime()})
			}
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].modTime.After(entries[j].modTime)
		})

		if len(entries) == 0 {
			return mcp.NewToolResultText("No matches found"), nil
		}

		var sb strings.Builder
		for _, e := range entries {
			sb.WriteString(e.path)
			sb.WriteByte('\n')
		}
		return mcp.NewToolResultText(strings.TrimRight(sb.String(), "\n")), nil
	})
}

// matchDoubleGlob matches a path against a pattern containing **.
// ** matches any number of path segments.
func matchDoubleGlob(pattern, path string) bool {
	parts := strings.Split(pattern, "**")
	var regexParts []string
	for i, part := range parts {
		escaped := regexp.QuoteMeta(part)
		escaped = strings.ReplaceAll(escaped, `\*`, `[^/]*`)
		escaped = strings.ReplaceAll(escaped, `\?`, `[^/]`)
		regexParts = append(regexParts, escaped)
		if i < len(parts)-1 {
			regexParts = append(regexParts, `.*`)
		}
	}
	re, err := regexp.Compile("^" + strings.Join(regexParts, "") + "$")
	if err != nil {
		return false
	}
	return re.MatchString(path)
}

var defaultSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".next":        true,
	"__pycache__":  true,
	".venv":        true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".cache":       true,
}

func registerGrep(srv *server.MCPServer, workingDir string) {
	tool := mcp.NewTool("galacta_grep",
		mcp.WithDescription("Search file contents using regex patterns"),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Regular expression pattern to search for")),
		mcp.WithString("path", mcp.Description("Directory to search in (default: working directory)")),
		mcp.WithString("glob", mcp.Description("File filter glob pattern (e.g. '*.go')")),
		mcp.WithNumber("context", mcp.Description("Number of context lines before and after each match")),
		mcp.WithString("output_mode", mcp.Description("Output mode: 'content' for matching lines or 'files_with_matches' for file paths only (default: files_with_matches)")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		pattern := mcp.ParseString(req, "pattern", "")
		searchPath := mcp.ParseString(req, "path", workingDir)
		globFilter := mcp.ParseString(req, "glob", "")
		contextLines := mcp.ParseInt(req, "context", 0)
		outputMode := mcp.ParseString(req, "output_mode", "files_with_matches")

		if !filepath.IsAbs(searchPath) {
			searchPath = filepath.Join(workingDir, searchPath)
		}
		searchPath = filepath.Clean(searchPath)

		re, err := regexp.Compile(pattern)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid regex pattern: %s", err.Error())), nil
		}

		const maxFiles = 100
		var matchedFiles []string
		var contentResults []string
		fileCount := 0

		err = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if fileCount >= maxFiles {
				return filepath.SkipAll
			}

			if info.IsDir() {
				if defaultSkipDirs[info.Name()] {
					return filepath.SkipDir
				}
				return nil
			}

			if info.Size() > 1024*1024 {
				return nil
			}

			if globFilter != "" {
				matched, err := filepath.Match(globFilter, info.Name())
				if err != nil || !matched {
					return nil
				}
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			checkLen := len(data)
			if checkLen > 512 {
				checkLen = 512
			}
			if isBinary(data[:checkLen]) {
				return nil
			}

			lines := strings.Split(string(data), "\n")
			var matchingLines []int
			for i, line := range lines {
				if re.MatchString(line) {
					matchingLines = append(matchingLines, i)
				}
			}

			if len(matchingLines) == 0 {
				return nil
			}

			fileCount++

			rel, err := filepath.Rel(searchPath, path)
			if err != nil {
				rel = path
			}

			if outputMode == "files_with_matches" {
				matchedFiles = append(matchedFiles, rel)
			} else {
				var fileOutput strings.Builder
				fileOutput.WriteString(fmt.Sprintf("=== %s ===\n", rel))

				showLines := make(map[int]bool)
				for _, lineIdx := range matchingLines {
					for c := lineIdx - contextLines; c <= lineIdx+contextLines; c++ {
						if c >= 0 && c < len(lines) {
							showLines[c] = true
						}
					}
				}

				sorted := make([]int, 0, len(showLines))
				for idx := range showLines {
					sorted = append(sorted, idx)
				}
				sort.Ints(sorted)

				lastLine := -2
				for _, idx := range sorted {
					if lastLine >= 0 && idx > lastLine+1 {
						fileOutput.WriteString("--\n")
					}
					fileOutput.WriteString(fmt.Sprintf("%6d\t%s\n", idx+1, lines[idx]))
					lastLine = idx
				}

				contentResults = append(contentResults, fileOutput.String())
			}

			return nil
		})

		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("searching files: %s", err.Error())), nil
		}

		if outputMode == "files_with_matches" {
			if len(matchedFiles) == 0 {
				return mcp.NewToolResultText("No matches found"), nil
			}
			return mcp.NewToolResultText(strings.Join(matchedFiles, "\n")), nil
		}

		if len(contentResults) == 0 {
			return mcp.NewToolResultText("No matches found"), nil
		}
		return mcp.NewToolResultText(strings.Join(contentResults, "\n")), nil
	})
}
