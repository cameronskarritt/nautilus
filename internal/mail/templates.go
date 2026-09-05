package mail

import (
	"embed"
	"io"
	"io/fs"
	"strings"

	"nautilus/internal/errors"
)

//go:embed templates/*.text.template templates/*.html.template
var templateFS embed.FS

type EmailTemplates struct {
	textTemplates executer
	htmlTemplates executer
}

type executer interface {
	ExecuteTemplate(wr io.Writer, name string, data any) error
}

func executeTemplate(tmpl executer, name string, data any) (string, error) {
	var buf strings.Builder

	err := tmpl.ExecuteTemplate(&buf, name, data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (et *EmailTemplates) ExecuteTemplate(name string, data any) (string, string, error) {
	plaintext, err := executeTemplate(et.textTemplates, name, data)
	if err != nil {
		return "", "", errors.Wrapf(err, "unable to execute plaintext template: %s", name)
	}

	htmlcontent, err := executeTemplate(et.htmlTemplates, name, data)
	if err != nil {
		return "", "", errors.Wrapf(err, "unable to execute html template: %s", name)
	}

	return plaintext, htmlcontent, nil
}

type templateCheck struct {
	hasSubject bool
	hasHTML    bool
	hasText    bool
}

func NewTemplates() (*EmailTemplates, error) {
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		return nil, errors.Wrap(err, "error reading template files")
	}

	templateChecks := make(map[string]*templateCheck)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		switch {
		case strings.HasSuffix(name, ".text.template"):
			templateName := strings.TrimSuffix(name, ".text.template")

			if templateChecks[templateName] == nil {
				templateChecks[templateName] = new(templateCheck)
			}
			templateChecks[templateName].hasText = true

		case strings.HasSuffix(name, ".html.template"):
			templateName := strings.TrimSuffix(name, ".html.template")

			if templateChecks[templateName] == nil {
				templateChecks[templateName] = new(templateCheck)
			}
			templateChecks[templateName].hasHTML = true
		}
	}

	textTemplates, err := loadTextTemplates(templateFS, entries)
	if err != nil {
		return nil, err
	}
	htmlTemplates, err := loadHTMLTemplates(templateFS, entries)
	if err != nil {
		return nil, err
	}
	templs := &EmailTemplates{
		textTemplates: textTemplates,
		htmlTemplates: htmlTemplates,
	}

	for subject := range subjects {
		templateName := subject.String()
		check, ok := templateChecks[templateName]
		if !ok {
			return nil, errors.Errorf("no template files found for subject: %s", templateName)
		}
		check.hasSubject = true
	}

	for templateName, check := range templateChecks {
		// Ignore partials
		if strings.HasPrefix(templateName, "_") {
			continue
		}

		if !check.hasSubject {
			return nil, errors.Errorf("no subject set for template: %s", templateName)
		}
		if !check.hasHTML {
			return nil, errors.Errorf("no html template found for subject: %s", templateName)
		}
		if !check.hasText {
			return nil, errors.Errorf("no text template found for subject: %s", templateName)
		}
	}

	return templs, nil
}
