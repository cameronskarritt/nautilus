package prompts

import (
	"embed"
	"io/fs"
	"strings"
	"text/template"

	"nautilus/internal/errors"
)

//go:embed all:templates/*.template
var templateFS embed.FS

var prompts *template.Template

func init() {
	t, err := newPrompts()
	if err != nil {
		panic(err)
	}

	prompts = t
}

func newPrompts() (*template.Template, error) {
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		return nil, errors.Wrap(err, "error reading template files")
	}

	root := template.New("__root")

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".template") {
			continue
		}

		filename := "templates/" + name
		content, err := templateFS.ReadFile(filename)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to read template file: %s", filename)
		}

		templateName := strings.TrimSuffix(name, ".template")

		child := root.New(templateName)
		_, err = child.Parse(string(content))
		if err != nil {
			return nil, errors.Wrapf(err, "unable to parse template: %s", templateName)
		}
	}

	return root, nil
}

// Execute executes a template by name with the provided data and returns the result as a string.
func Execute(name string, data any) (string, error) {
	var buf strings.Builder

	err := prompts.ExecuteTemplate(&buf, name, data)
	if err != nil {
		return "", errors.Wrapf(err, "unable to execute template: %s", name)
	}

	return buf.String(), nil
}
