package httputil

import "reflect"

// Group type-strings into their equivalent or similar JSON type.
// For types that don't exactly match a Javascript type, use a language
// agnostic type-string.
func kindGroup(k reflect.Kind) string {
	switch k {
	case reflect.Bool:
		return "bool"

	case reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64:
		return "int"

	case reflect.Uint,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64:
		return "uint"

	case reflect.Float32,
		reflect.Float64:
		return "float"

	case reflect.Map,
		reflect.Struct:
		return "object"

	case reflect.String:
		return "string"

	default:
		return k.String()
	}
}
