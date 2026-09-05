package mail

import (
	"bytes"
	"context"
	"io"
	"testing"

	"nautilus/internal/testutil/require"
)

func TestLocalSender(t *testing.T) {
	t.Parallel()

	buf := new(bytes.Buffer)
	sender, err := newLocalSender(buf, "From: {{ .From }}; To: {{ range .To }}{{ . }} {{ end }}; Subject: {{ .Subject }}; Body: {{ .Plaintext }}")
	require.NoError(t, err)

	err = sender.Send(context.Background(), &Message{
		From:      "sender@example.com",
		To:        []string{"one@example.com", "two@example.com"},
		Subject:   "Hello",
		Plaintext: "Welcome",
	})
	require.NoError(t, err)
	require.Equal(t, "From: sender@example.com; To: one@example.com two@example.com ; Subject: Hello; Body: Welcome", buf.String())
}

func TestLocalSenderErrors(t *testing.T) {
	t.Parallel()

	_, err := newLocalSender(io.Discard, "{{.InvalidSyntax")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unable to create local sender")

	sender, err := newLocalSender(io.Discard, "{{ .Missing }}")
	require.NoError(t, err)
	err = sender.Send(context.Background(), new(Message))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unable to execute local sender template")
}
