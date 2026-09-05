package config

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"nautilus/internal/testutil/require"
)

type mapProvider map[string]string

func (p mapProvider) Get(key string) (string, bool) {
	value, ok := p[key]
	return value, ok
}

func withProvider(provider Provider) func() {
	SetProvider(provider)
	return func() {
		SetProvider(new(EnvProvider))
	}
}

func TestGet(t *testing.T) {
	defer withProvider(mapProvider{
		"STRING_KEY":    "string_value",
		"INT_KEY":       "123",
		"FLOAT_KEY":     "123.45",
		"BOOL_KEY":      "true",
		"INVALID_INT":   "not_a_number",
		"INVALID_FLOAT": "not_a_float",
		"INVALID_BOOL":  "not_a_bool",
	})()

	stringTests := []struct {
		Name     string
		Key      string
		Expected string
	}{
		{Name: "string value", Key: "STRING_KEY", Expected: "string_value"},
		{Name: "missing string", Key: "MISSING_STRING", Expected: ""},
	}

	for _, tt := range stringTests {
		t.Run(tt.Name, func(t *testing.T) {
			require.Equal(t, tt.Expected, Get[string](tt.Key))
		})
	}

	intTests := []struct {
		Name     string
		Key      string
		Expected int
	}{
		{Name: "int value", Key: "INT_KEY", Expected: 123},
		{Name: "missing int", Key: "MISSING_INT", Expected: 0},
		{Name: "invalid int", Key: "INVALID_INT", Expected: 0},
	}

	for _, tt := range intTests {
		t.Run(tt.Name, func(t *testing.T) {
			require.Equal(t, tt.Expected, Get[int](tt.Key))
		})
	}

	floatTests := []struct {
		Name     string
		Key      string
		Expected float64
	}{
		{Name: "float value", Key: "FLOAT_KEY", Expected: 123.45},
		{Name: "missing float", Key: "MISSING_FLOAT", Expected: 0},
		{Name: "invalid float", Key: "INVALID_FLOAT", Expected: 0},
	}

	for _, tt := range floatTests {
		t.Run(tt.Name, func(t *testing.T) {
			require.Equal(t, tt.Expected, Get[float64](tt.Key))
		})
	}

	boolTests := []struct {
		Name     string
		Key      string
		Expected bool
	}{
		{Name: "bool value", Key: "BOOL_KEY", Expected: true},
		{Name: "missing bool", Key: "MISSING_BOOL", Expected: false},
		{Name: "invalid bool", Key: "INVALID_BOOL", Expected: false},
	}

	for _, tt := range boolTests {
		t.Run(tt.Name, func(t *testing.T) {
			require.Equal(t, tt.Expected, Get[bool](tt.Key))
		})
	}
}

func TestGetFallback(t *testing.T) {
	defer withProvider(mapProvider{
		"STRING_KEY":    "string_value",
		"INT_KEY":       "123",
		"FLOAT_KEY":     "123.45",
		"BOOL_KEY":      "true",
		"INVALID_INT":   "not_a_number",
		"INVALID_FLOAT": "not_a_float",
		"INVALID_BOOL":  "not_a_bool",
	})()

	stringTests := []struct {
		Name     string
		Key      string
		Fallback string
		Expected string
	}{
		{Name: "existing string", Key: "STRING_KEY", Fallback: "default", Expected: "string_value"},
		{Name: "missing string", Key: "MISSING_STRING", Fallback: "default", Expected: "default"},
	}

	for _, tt := range stringTests {
		t.Run(tt.Name, func(t *testing.T) {
			require.Equal(t, tt.Expected, Get(tt.Key, tt.Fallback))
		})
	}

	intTests := []struct {
		Name     string
		Key      string
		Fallback int
		Expected int
	}{
		{Name: "existing int", Key: "INT_KEY", Fallback: 456, Expected: 123},
		{Name: "missing int", Key: "MISSING_INT", Fallback: 456, Expected: 456},
		{Name: "invalid int", Key: "INVALID_INT", Fallback: 456, Expected: 456},
	}

	for _, tt := range intTests {
		t.Run(tt.Name, func(t *testing.T) {
			require.Equal(t, tt.Expected, Get(tt.Key, tt.Fallback))
		})
	}

	floatTests := []struct {
		Name     string
		Key      string
		Fallback float64
		Expected float64
	}{
		{Name: "existing float", Key: "FLOAT_KEY", Fallback: 456.78, Expected: 123.45},
		{Name: "missing float", Key: "MISSING_FLOAT", Fallback: 456.78, Expected: 456.78},
		{Name: "invalid float", Key: "INVALID_FLOAT", Fallback: 456.78, Expected: 456.78},
	}

	for _, tt := range floatTests {
		t.Run(tt.Name, func(t *testing.T) {
			require.Equal(t, tt.Expected, Get(tt.Key, tt.Fallback))
		})
	}

	boolTests := []struct {
		Name     string
		Key      string
		Fallback bool
		Expected bool
	}{
		{Name: "existing bool", Key: "BOOL_KEY", Fallback: false, Expected: true},
		{Name: "missing bool", Key: "MISSING_BOOL", Fallback: true, Expected: true},
		{Name: "invalid bool", Key: "INVALID_BOOL", Fallback: true, Expected: true},
	}

	for _, tt := range boolTests {
		t.Run(tt.Name, func(t *testing.T) {
			require.Equal(t, tt.Expected, Get(tt.Key, tt.Fallback))
		})
	}
}

func TestSetProviderClearsCache(t *testing.T) {
	defer withProvider(mapProvider{"KEY": "old"})()
	require.Equal(t, "old", Get[string]("KEY"))

	SetProvider(mapProvider{"KEY": "new"})
	require.Equal(t, "new", Get[string]("KEY"))
}

func TestParseDotenv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Input    string
		Expected map[string]string
	}{
		{
			Name:  "simple key value",
			Input: "FOO=bar",
			Expected: map[string]string{
				"FOO": "bar",
			},
		},
		{
			Name:  "multiple key values",
			Input: "FOO=bar\nBAZ=qux",
			Expected: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
		{
			Name:  "skip empty lines",
			Input: "FOO=bar\n\nBAZ=qux",
			Expected: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
		{
			Name:  "skip comments",
			Input: "# this is a comment\nFOO=bar\n# another comment\nBAZ=qux",
			Expected: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
		{
			Name:  "trim whitespace around key and value",
			Input: "  FOO  =  bar  ",
			Expected: map[string]string{
				"FOO": "bar",
			},
		},
		{
			Name:  "double quoted value",
			Input: `FOO="bar baz"`,
			Expected: map[string]string{
				"FOO": "bar baz",
			},
		},
		{
			Name:  "single quoted value",
			Input: `FOO='bar baz'`,
			Expected: map[string]string{
				"FOO": "bar baz",
			},
		},
		{
			Name:  "value with equals sign",
			Input: "FOO=bar=baz",
			Expected: map[string]string{
				"FOO": "bar=baz",
			},
		},
		{
			Name:  "empty value",
			Input: "FOO=",
			Expected: map[string]string{
				"FOO": "",
			},
		},
		{
			Name:     "line without equals is skipped",
			Input:    "FOOBAR",
			Expected: map[string]string{},
		},
		{
			Name:     "empty input",
			Input:    "",
			Expected: map[string]string{},
		},
		{
			Name:  "quoted empty value",
			Input: `FOO=""`,
			Expected: map[string]string{
				"FOO": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			values := make(map[string]string)
			scanner := bufio.NewScanner(strings.NewReader(tt.Input))
			parseDotenv(scanner, values)

			require.Equal(t, tt.Expected, values)
		})
	}
}

func TestParseDotenvDoesNotOverrideEnv(t *testing.T) {
	err := os.Setenv("EXISTING_KEY", "from_env")
	require.NoError(t, err)
	defer os.Unsetenv("EXISTING_KEY")

	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader("EXISTING_KEY=from_dotenv\nNEW_KEY=new_value"))
	parseDotenv(scanner, values)

	require.Equal(t, map[string]string{"NEW_KEY": "new_value"}, values)
}
