package mail

import (
	"io/fs"
	"strings"
	"text/template"

	"nautilus/internal/errors"
)

func loadTextTemplates(templateFS fs.ReadFileFS, entries []fs.DirEntry) (*template.Template, error) {
	templates := template.New("__root").Funcs(funcs)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".text.template") {
			continue
		}

		filename := "templates/" + name
		content, err := templateFS.ReadFile(filename)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to read template file: %s", filename)
		}

		templateName := strings.TrimSuffix(name, ".text.template")
		if _, err := templates.New(templateName).Parse(string(content)); err != nil {
			return nil, errors.Wrapf(err, "unable to parse text template: %s", templateName)
		}
	}
	return templates, nil
}
