package systemprompt

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed prompt.tmpl
var promptFS embed.FS

// templateData holds all data passed to the system prompt template.
type templateData struct {
	Env          EnvContext
	ClaudeMDFiles []ClaudeMDFile
	ToolNames    []string
}

var promptTemplate *template.Template

func init() {
	content, err := promptFS.ReadFile("prompt.tmpl")
	if err != nil {
		panic("systemprompt: failed to read embedded template: " + err.Error())
	}
	promptTemplate = template.Must(template.New("prompt").Parse(string(content)))
}

// renderTemplate executes the system prompt template with the given data.
func renderTemplate(data templateData) (string, error) {
	var buf bytes.Buffer
	if err := promptTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
