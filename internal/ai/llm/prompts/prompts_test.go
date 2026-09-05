package prompts

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestExecute(t *testing.T) {
	t.Parallel()

	prompt, err := Execute("repair", struct {
		ToolName          string
		ToolDescription   string
		ExpectedSchema    string
		Error             string
		OriginalArguments string
	}{
		ToolName:          "search",
		ToolDescription:   "Search documents",
		ExpectedSchema:    `{"type":"object"}`,
		Error:             "missing query",
		OriginalArguments: `{}`,
	})
	require.NoError(t, err)
	require.Contains(t, prompt, "Tool name: search")
	require.Contains(t, prompt, "Tool description: Search documents")
	require.Contains(t, prompt, `Expected schema: {"type":"object"}`)
	require.Contains(t, prompt, "Error: missing query")
	require.Contains(t, prompt, "Original arguments:\n{}")
}

func TestExecuteMissingTemplate(t *testing.T) {
	t.Parallel()

	_, err := Execute("missing", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unable to execute template: missing")
}
