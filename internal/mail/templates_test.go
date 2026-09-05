package mail

import (
	"testing"

	"nautilus/internal/enums"
	"nautilus/internal/testutil/require"
)

func TestGetSubject(t *testing.T) {
	t.Parallel()

	subject, err := GetSubject(enums.MailTemplateWelcome)
	require.NoError(t, err)
	require.Equal(t, "Welcome to nautilus!", subject)

	_, err = GetSubject(enums.MailTemplate("missing"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "no subject set for template: missing")
}

func TestNewTemplates(t *testing.T) {
	t.Parallel()

	templates, err := NewTemplates()
	require.NoError(t, err)

	text, html, err := templates.ExecuteTemplate(enums.MailTemplateWelcome.String(), nil)
	require.NoError(t, err)
	require.Contains(t, text, "Welcome aboard")
	require.Contains(t, html, "<h1>Welcome aboard")
	require.Contains(t, html, "support@example.com")
}
