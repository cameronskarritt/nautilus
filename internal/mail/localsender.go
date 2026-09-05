package mail

import (
	"context"
	"io"
	"os"
	"text/template"

	"nautilus/internal/errors"
)

type LocalSender struct {
	w    io.Writer
	tmpl *template.Template
}

var _ Sender = new(LocalSender)

const localTemplate = `
From: {{ .From }}
To: [{{ range $index, $to := .To }}{{ if $index }}, {{ end }}"{{ $to }}"{{ end }}]
Subject: {{ .Subject }}

{{ .Plaintext }}
---
`

func NewLocalSender() (*LocalSender, error) {
	return newLocalSender(os.Stdout, localTemplate)
}

func newLocalSender(writer io.Writer, t string) (*LocalSender, error) {
	tmpl, err := template.New("").Parse(t)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create local sender")
	}

	return &LocalSender{
		w:    writer,
		tmpl: tmpl,
	}, nil
}

func (ls *LocalSender) Send(_ context.Context, message *Message) error {
	err := ls.tmpl.Execute(ls.w, message)
	if err != nil {
		return errors.Wrap(err, "unable to execute local sender template")
	}
	return nil
}
