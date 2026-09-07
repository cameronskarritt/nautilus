package keys

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want *options
	}{
		{name: "user", args: []string{"provision-user", "--key-arn", "arn"}, want: &options{command: "provision-user", keyARN: "arn"}},
		{name: "organization", args: []string{"provision-organization", "--key-arn", "arn", "--org-id", "org"}, want: &options{command: "provision-organization", keyARN: "arn", orgID: "org"}},
		{name: "no command"},
		{name: "unknown command", args: []string{"rotate"}},
		{name: "missing key", args: []string{"provision-user"}},
		{name: "missing organization", args: []string{"provision-organization", "--key-arn", "arn"}},
		{name: "unexpected argument", args: []string{"provision-user", "--key-arn", "arn", "extra"}},
		{name: "user organization flag", args: []string{"provision-user", "--key-arn", "arn", "--org-id", "org"}},
		{name: "plaintext key argument", args: []string{"provision-user", "--key-arn", "arn", "--key", "secret"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parse(tt.args)
			if tt.want == nil {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
