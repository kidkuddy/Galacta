package systemprompt

import "strings"

// BuildOptions configures system prompt construction.
type BuildOptions struct {
	WorkingDir   string   // session working directory
	Model        string   // model name
	ToolNames    []string // available tool names
	UserOverride string   // user-provided system prompt (appended)
}

// Build assembles the full system prompt from template, environment, CLAUDE.md files,
// and an optional user override.
func Build(opts BuildOptions) (string, error) {
	env := CollectEnv(opts.WorkingDir, opts.Model)
	claudeFiles := DiscoverClaudeMD(opts.WorkingDir)

	data := templateData{
		Env:           env,
		ClaudeMDFiles: claudeFiles,
		ToolNames:     opts.ToolNames,
	}

	prompt, err := renderTemplate(data)
	if err != nil {
		return "", err
	}

	prompt = strings.TrimSpace(prompt)

	if opts.UserOverride != "" {
		prompt += "\n\n# User Instructions\n\n" + opts.UserOverride
	}

	return prompt, nil
}
