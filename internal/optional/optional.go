package optional

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"

	"nautilus/internal/errors"
)

type Optional[T any] struct {
	Set  bool `json:"-"`
	Data T
}

func Set[T any](val T) Optional[T] {
	return Optional[T]{
		Set:  true,
		Data: val,
	}
}

func Empty[T any]() Optional[T] {
	return Optional[T]{
		Set: false,
	}
}

func (o *Optional[T]) IsZero() bool {
	return !o.Set
}

func (o *Optional[T]) Or(val T) T {
	if o.Set {
		return o.Data
	}
	return val
}

// IsSet and GetValue satisfy querybuilder.OptionalValue.
func (o Optional[T]) IsSet() bool {
	return o.Set
}

func (o Optional[T]) GetValue() any {
	return o.Data
}

func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if err := json.Unmarshal(data, &o.Data); err != nil {
		return errors.Wrap(err, "error unmarshalling JSON")
	}

	return nil
}

func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if !o.Set {
		return []byte("null"), nil
	}

	buf, err := json.Marshal(o.Data)
	if err != nil {
		return nil, errors.Wrap(err, "error marshalling JSON")
	}
	return buf, nil
}

func (o Optional[T]) Value() (driver.Value, error) {
	if !o.Set {
		return nil, nil
	}

	if valuer, ok := any(o.Data).(driver.Valuer); ok {
		value, err := valuer.Value()
		if err != nil {
			return nil, errors.Wrap(err, "error getting value")
		}
		return value, nil
	}

	return o.Data, nil
}

func (o *Optional[T]) Scan(value any) error {
	if value == nil {
		o.Set = false
		return nil
	}

	if scanner, ok := any(o.Data).(sql.Scanner); ok {
		err := scanner.Scan(value)
		if err != nil {
			return errors.Wrap(err, "error scanning value")
		}
		o.Set = true
		return nil
	}

	if v, ok := value.(T); ok {
		o.Data = v
		o.Set = true
		return nil
	}

	if converted, ok := convertNumeric[T](value); ok {
		o.Data = converted
		o.Set = true
		return nil
	}

	return errors.Errorf("type does not match optional: %T into %T", value, o.Data)
}

// Database drivers return int64/float64 for numeric columns.
func convertNumeric[T any](value any) (T, bool) {
	var zero T

	switch v := value.(type) {
	case int64:
		switch any(zero).(type) {
		case int:
			return any(int(v)).(T), true
		case int32:
			return any(int32(v)).(T), true
		case int64:
			return any(v).(T), true
		}
	case float64:
		switch any(zero).(type) {
		case float32:
			return any(float32(v)).(T), true
		case float64:
			return any(v).(T), true
		}
	}

	return zero, false
}
