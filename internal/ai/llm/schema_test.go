package llm

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestSchema_Validate(t *testing.T) {
	t.Parallel()

	allowExtra := true
	strict := false
	userSchema := &Schema{
		Type:     TypeObject,
		Required: []string{"name", "age"},
		Properties: Properties{
			"name": S(""),
			"age":  Int(""),
			"tags": {Type: TypeArray, Items: S("")},
			"size": {Type: TypeString, Enum: []any{"small", "medium", "large"}},
		},
		AdditionalProperties: &strict,
	}

	tests := []struct {
		Name          string
		Schema        *Schema
		Input         string
		ErrorContains string
	}{
		{
			Name:   "valid object",
			Schema: userSchema,
			Input:  `{"name":"Alice","age":30,"tags":["go","ai"],"size":"medium"}`,
		},
		{
			Name:          "missing required field",
			Schema:        userSchema,
			Input:         `{"name":"Alice"}`,
			ErrorContains: `missing required field "age"`,
		},
		{
			Name:          "unexpected property",
			Schema:        userSchema,
			Input:         `{"name":"Alice","age":30,"extra":true}`,
			ErrorContains: `unexpected property "extra"`,
		},
		{
			Name: "allows additional properties",
			Schema: &Schema{
				Type:                 TypeObject,
				Properties:           Properties{"name": S("")},
				AdditionalProperties: &allowExtra,
			},
			Input: `{"name":"Alice","extra":true}`,
		},
		{
			Name:          "wrong primitive type",
			Schema:        userSchema,
			Input:         `{"name":123,"age":30}`,
			ErrorContains: "name: expected string",
		},
		{
			Name:          "integer rejects floats",
			Schema:        userSchema,
			Input:         `{"name":"Alice","age":30.5}`,
			ErrorContains: "age: expected integer",
		},
		{
			Name:          "array item type",
			Schema:        userSchema,
			Input:         `{"name":"Alice","age":30,"tags":["go",1]}`,
			ErrorContains: "tags[1]: expected string",
		},
		{
			Name:          "enum value",
			Schema:        userSchema,
			Input:         `{"name":"Alice","age":30,"size":"xlarge"}`,
			ErrorContains: `size: value "xlarge" not in enum`,
		},
		{
			Name: "nested object path",
			Schema: &Schema{
				Type:     TypeObject,
				Required: []string{"user"},
				Properties: Properties{
					"user": {
						Type:       TypeObject,
						Required:   []string{"name"},
						Properties: Properties{"name": S("")},
					},
				},
			},
			Input:         `{"user":{"name":123}}`,
			ErrorContains: "user.name: expected string",
		},
		{
			Name:   "null optional field",
			Schema: userSchema,
			Input:  `{"name":"Alice","age":30,"tags":null}`,
		},
		{
			Name: "empty object input",
			Schema: &Schema{
				Type:                 TypeObject,
				AdditionalProperties: &strict,
			},
		},
		{
			Name:          "empty non-object input",
			Schema:        S(""),
			ErrorContains: "invalid JSON: empty input",
		},
		{
			Name:          "invalid json",
			Schema:        userSchema,
			Input:         `{invalid}`,
			ErrorContains: "invalid JSON",
		},
		{
			Name:          "unknown schema type",
			Schema:        &Schema{Type: "mystery"},
			Input:         `"value"`,
			ErrorContains: `unknown type "mystery"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			err := tt.Schema.Validate([]byte(tt.Input))
			if tt.ErrorContains == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.ErrorContains)
		})
	}
}
