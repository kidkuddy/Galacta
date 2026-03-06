package systemprompt

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed prompt_static.tmpl
var staticFS embed.FS

//go:embed prompt_dynamic.tmpl
var dynamicFS embed.FS

// templateData holds all data passed to the system prompt templates.
type templateData struct {
	Env           EnvContext
	ClaudeMDFiles []ClaudeMDFile
	ToolNames     []string
}

var (
	staticTemplate  *template.Template
	dynamicTemplate *template.Template
)

func init() {
	staticContent, err := staticFS.ReadFile("prompt_static.tmpl")
	if err != nil {
		panic("systemprompt: failed to read embedded static template: " + err.Error())
	}
	staticTemplate = template.Must(template.New("static").Parse(string(staticContent)))

	dynamicContent, err := dynamicFS.ReadFile("prompt_dynamic.tmpl")
	if err != nil {
		panic("systemprompt: failed to read embedded dynamic template: " + err.Error())
	}
	dynamicTemplate = template.Must(template.New("dynamic").Parse(string(dynamicContent)))
}

// renderStaticTemplate executes the static system prompt template.
func renderStaticTemplate(data templateData) (string, error) {
	var buf bytes.Buffer
	if err := staticTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderDynamicTemplate executes the dynamic system prompt template.
func renderDynamicTemplate(data templateData) (string, error) {
	var buf bytes.Buffer
	if err := dynamicTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
