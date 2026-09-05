package llm

import (
	"encoding/json"
	"fmt"

	"nautilus/internal/errors"
)

// Type constants for JSON Schema types
const (
	TypeString  = "string"
	TypeNumber  = "number"
	TypeInteger = "integer"
	TypeBool    = "boolean"
	TypeArray   = "array"
	TypeObject  = "object"
)

// Schema represents a JSON Schema definition
type Schema struct {
	Type                 string     `json:"type,omitempty"`
	Description          string     `json:"description,omitempty"`
	Properties           Properties `json:"properties,omitempty"`
	Required             []string   `json:"required,omitempty"`
	Items                *Schema    `json:"items,omitempty"`
	Enum                 []any      `json:"enum,omitempty"`
	AdditionalProperties *bool      `json:"additionalProperties,omitempty"`
}

func String(desc string) *Schema {
	return &Schema{Type: TypeString, Description: desc}
}

func S(desc string) *Schema {
	return String(desc)
}

func Int(desc string) *Schema {
	return &Schema{Type: TypeInteger, Description: desc}
}

func Bool(desc string) *Schema {
	return &Schema{Type: TypeBool, Description: desc}
}

type Properties map[string]*Schema

func (s *Schema) Validate(data []byte) error {
	var value any

	// Handle empty input: treat as empty object for object schemas
	if len(data) == 0 {
		if s.Type == TypeObject {
			value = map[string]any{}
		} else {
			return errors.New("invalid JSON: empty input")
		}
	} else {
		if err := json.Unmarshal(data, &value); err != nil {
			return errors.Wrap(err, "invalid JSON")
		}
	}

	return s.validateValue("", value)
}

func (s *Schema) validateValue(path string, value any) error {
	// Handle null values - they're allowed for optional fields
	if value == nil {
		return nil
	}

	switch s.Type {
	case TypeObject:
		return s.validateObject(path, value)
	case TypeArray:
		return s.validateArray(path, value)
	case TypeString:
		return s.validateString(path, value)
	case TypeNumber:
		return s.validateNumber(path, value)
	case TypeInteger:
		return s.validateInteger(path, value)
	case TypeBool:
		return s.validateBool(path, value)
	case "":
		// No type specified, allow any value
		return nil
	default:
		return errors.Errorf("%s: unknown type %q", path, s.Type)
	}
}

func (s *Schema) validateObject(path string, value any) error {
	obj, ok := value.(map[string]any)
	if !ok {
		return errors.Errorf("%s: expected object, got %T", pathOrRoot(path), value)
	}

	// Check required fields
	for _, req := range s.Required {
		if _, exists := obj[req]; !exists {
			return errors.Errorf("%s: missing required field %q", pathOrRoot(path), req)
		}
	}

	// Check for extra properties
	allowExtra := s.AdditionalProperties != nil && *s.AdditionalProperties
	if !allowExtra {
		for key := range obj {
			if s.Properties != nil {
				if _, defined := s.Properties[key]; !defined {
					return errors.Errorf("%s: unexpected property %q", pathOrRoot(path), key)
				}
			}
		}
	}

	// Validate each property
	for key, propSchema := range s.Properties {
		propValue, exists := obj[key]
		if !exists {
			continue // Optional field not present
		}

		propPath := key
		if path != "" {
			propPath = path + "." + key
		}

		if err := propSchema.validateValue(propPath, propValue); err != nil {
			return err
		}
	}

	return nil
}

func (s *Schema) validateArray(path string, value any) error {
	arr, ok := value.([]any)
	if !ok {
		return errors.Errorf("%s: expected array, got %T", pathOrRoot(path), value)
	}

	if s.Items == nil {
		return nil // No item schema, allow any items
	}

	for i, item := range arr {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if path == "" {
			itemPath = fmt.Sprintf("[%d]", i)
		}

		if err := s.Items.validateValue(itemPath, item); err != nil {
			return err
		}
	}

	return nil
}

func (s *Schema) validateString(path string, value any) error {
	str, ok := value.(string)
	if !ok {
		return errors.Errorf("%s: expected string, got %T", pathOrRoot(path), value)
	}

	if len(s.Enum) > 0 {
		found := false
		for _, e := range s.Enum {
			if e == str {
				found = true
				break
			}
		}
		if !found {
			return errors.Errorf("%s: value %q not in enum %v", pathOrRoot(path), str, s.Enum)
		}
	}

	return nil
}

func (s *Schema) validateNumber(path string, value any) error {
	_, ok := value.(float64)
	if !ok {
		return errors.Errorf("%s: expected number, got %T", pathOrRoot(path), value)
	}
	return nil
}

func (s *Schema) validateInteger(path string, value any) error {
	num, ok := value.(float64)
	if !ok {
		return errors.Errorf("%s: expected integer, got %T", pathOrRoot(path), value)
	}

	// Check if it's a whole number
	if num != float64(int64(num)) {
		return errors.Errorf("%s: expected integer, got float %v", pathOrRoot(path), num)
	}

	return nil
}

func (s *Schema) validateBool(path string, value any) error {
	_, ok := value.(bool)
	if !ok {
		return errors.Errorf("%s: expected boolean, got %T", pathOrRoot(path), value)
	}
	return nil
}

func pathOrRoot(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

// StructuredOutput defines a structured output schema for LLM responses
type StructuredOutput struct {
	Name   string        `json:"name"`
	Schema *SchemaObject `json:"schema"`
	Strict bool          `json:"strict"`
}

// SchemaObject is used for structured output (different from tool parameters)
type SchemaObject struct {
	Type                 string     `json:"type,omitempty"`
	Name                 string     `json:"name,omitempty"`
	Description          string     `json:"description,omitempty"`
	Properties           Properties `json:"properties,omitempty"`
	Required             []string   `json:"required,omitempty"`
	AdditionalProperties bool       `json:"additionalProperties"`
}
