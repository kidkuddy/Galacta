package skill

// SkillDef defines a skill with a name, description, and prompt template.
// The Prompt field is a Go text/template that receives .Args as context.
type SkillDef struct {
	Name        string
	Description string
	Prompt      string
}

var builtinSkills = []SkillDef{
	{
		Name:        "commit",
		Description: "Create a git commit with a well-crafted message",
		Prompt: `Create a git commit for the current changes. Follow these steps:

1. Run git status and git diff to understand the changes
2. Draft a concise commit message that describes the "why" not just the "what"
3. Stage the relevant files and create the commit
{{if .Args}}
Additional instructions: {{.Args}}
{{end}}`,
	},
	{
		Name:        "review-pr",
		Description: "Review a pull request",
		Prompt: `Review the pull request. Follow these steps:

1. Understand the PR changes by examining the diff
2. Check for bugs, security issues, and code quality
3. Provide constructive feedback with specific suggestions
{{if .Args}}
PR reference: {{.Args}}
{{end}}`,
	},
}
