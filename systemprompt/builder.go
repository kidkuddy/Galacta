package systemprompt

import (
	"strings"

	"github.com/kidkuddy/galacta/anthropic"
)

// BuildOptions configures system prompt construction.
type BuildOptions struct {
	WorkingDir   string   // session working directory
	Model        string   // model name
	ToolNames    []string // available tool names
	UserOverride string   // user-provided system prompt (appended to dynamic section)
	MemoryBlock  string   // optional memory content to inject as a dynamic section
}

// Build assembles the full system prompt as a slice of SystemBlocks.
// The static portion (identity, instructions, tool docs) is marked with cache_control
// so the API can cache it across turns. The dynamic portion (env, CLAUDE.md, memory,
// user overrides) changes per session and is not cached.
func Build(opts BuildOptions) ([]anthropic.SystemBlock, error) {
	env := CollectEnv(opts.WorkingDir, opts.Model)
	claudeFiles := DiscoverClaudeMD(opts.WorkingDir)

	data := templateData{
		Env:           env,
		ClaudeMDFiles: claudeFiles,
		ToolNames:     opts.ToolNames,
	}

	staticPrompt, err := renderStaticTemplate(data)
	if err != nil {
		return nil, err
	}
	staticPrompt = strings.TrimSpace(staticPrompt)

	dynamicPrompt, err := renderDynamicTemplate(data)
	if err != nil {
		return nil, err
	}
	dynamicPrompt = strings.TrimSpace(dynamicPrompt)

	// Append optional sections to the dynamic block
	if opts.MemoryBlock != "" {
		dynamicPrompt += "\n\n" + opts.MemoryBlock
	}
	if opts.UserOverride != "" {
		dynamicPrompt += "\n\n# User Instructions\n\n" + opts.UserOverride
	}

	var blocks []anthropic.SystemBlock

	// Static block with cache control (stays the same across turns)
	blocks = append(blocks, anthropic.NewCachedSystemBlock(staticPrompt))

	// Dynamic block without cache control (changes per session)
	if dynamicPrompt != "" {
		blocks = append(blocks, anthropic.NewSystemBlock(dynamicPrompt))
	}

	return blocks, nil
}
