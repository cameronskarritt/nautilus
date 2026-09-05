package optional

import (
	"database/sql/driver"
	"encoding/json"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestOptional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name         string
		Optional     Optional[int]
		Default      int
		Expected     int
		ExpectedOr   int
		ExpectedSet  bool
		ExpectedZero bool
	}{
		{
			Name:         "set value",
			Optional:     Set(42),
			Default:      99,
			Expected:     42,
			ExpectedOr:   42,
			ExpectedSet:  true,
			ExpectedZero: false,
		},
		{
			Name:         "empty value",
			Optional:     Empty[int](),
			Default:      99,
			Expected:     0,
			ExpectedOr:   99,
			ExpectedSet:  false,
			ExpectedZero: true,
		},
		{
			Name:         "set zero value",
			Optional:     Set(0),
			Default:      99,
			Expected:     0,
			ExpectedOr:   0,
			ExpectedSet:  true,
			ExpectedZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.ExpectedSet, tt.Optional.Set)
			require.Equal(t, tt.Expected, tt.Optional.Data)
			require.Equal(t, tt.ExpectedZero, tt.Optional.IsZero())
			require.Equal(t, tt.ExpectedSet, tt.Optional.IsSet())
			require.Equal(t, tt.Expected, tt.Optional.GetValue())
			require.Equal(t, tt.ExpectedOr, tt.Optional.Or(tt.Default))
		})
	}
}

func TestMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Optional Optional[int]
		Expected string
	}{
		{Name: "set value", Optional: Set(123), Expected: "123"},
		{Name: "empty value", Optional: Empty[int](), Expected: "null"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			actual, err := tt.Optional.MarshalJSON()
			require.NoError(t, err)
			require.Equal(t, tt.Expected, string(actual))
		})
	}
}

func TestMarshalJSONReturnsError(t *testing.T) {
	t.Parallel()

	_, err := Set(make(chan int)).MarshalJSON()
	require.Error(t, err)
}

func TestUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Data          string
		Expected      Optional[int]
		ExpectedError bool
	}{
		{Name: "valid value", Data: "123", Expected: Set(123)},
		{Name: "invalid value", Data: `"abc"`, ExpectedError: true},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			var actual Optional[int]
			err := json.Unmarshal([]byte(tt.Data), &actual)
			if tt.ExpectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, actual)
		})
	}
}

type valuer struct {
	value driver.Value
	err   error
}

func (v valuer) Value() (driver.Value, error) {
	return v.value, v.err
}

type valueTest[T any] struct {
	Name          string
	Optional      Optional[T]
	Expected      driver.Value
	ExpectedError bool
}

func runValueTests[T any](t *testing.T, tests []valueTest[T]) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			actual, err := tt.Optional.Value()
			if tt.ExpectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, actual)
		})
	}
}

func TestValue(t *testing.T) {
	t.Parallel()

	runValueTests(t, []valueTest[int]{
		{Name: "empty int", Optional: Empty[int](), Expected: nil},
		{Name: "set int", Optional: Set(42), Expected: 42},
	})

	runValueTests(t, []valueTest[string]{
		{Name: "empty string", Optional: Empty[string](), Expected: nil},
		{Name: "set string", Optional: Set("hello"), Expected: "hello"},
	})

	runValueTests(t, []valueTest[valuer]{
		{
			Name:     "valuer success",
			Optional: Set(valuer{value: "custom value"}),
			Expected: "custom value",
		},
		{
			Name:          "valuer error",
			Optional:      Set(valuer{err: errors.New("valuer error")}),
			ExpectedError: true,
		},
	})
}

type scanTest[T comparable] struct {
	Name          string
	Input         any
	Initial       Optional[T]
	Expected      Optional[T]
	ExpectedError bool
}

func runScanTests[T comparable](t *testing.T, tests []scanTest[T]) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			actual := tt.Initial
			err := actual.Scan(tt.Input)
			if tt.ExpectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, actual)
		})
	}
}

func TestScan(t *testing.T) {
	t.Parallel()

	runScanTests(t, []scanTest[int]{
		{Name: "nil int", Input: nil, Expected: Empty[int]()},
		{Name: "int64 into int", Input: int64(123), Expected: Set(123)},
		{Name: "string into int", Input: "string value", ExpectedError: true},
	})

	runScanTests(t, []scanTest[string]{
		{Name: "nil string", Input: nil, Expected: Empty[string]()},
		{Name: "string into string", Input: "test string", Expected: Set("test string")},
		{Name: "int64 into string", Input: int64(123), ExpectedError: true},
	})

	runScanTests(t, []scanTest[float64]{
		{Name: "float64 into float64", Input: float64(3.14159), Expected: Set(3.14159)},
	})

	runScanTests(t, []scanTest[float32]{
		{Name: "float64 into float32", Input: float64(3.14), Expected: Set(float32(3.14))},
	})

	runScanTests(t, []scanTest[int32]{
		{Name: "int64 into int32", Input: int64(42), Expected: Set(int32(42))},
	})

	runScanTests(t, []scanTest[int64]{
		{Name: "int64 into int64", Input: int64(9999999999), Expected: Set(int64(9999999999))},
	})
}

type scanner struct {
	value any
	err   error
}

func (s *scanner) Scan(value any) error {
	if s.err != nil {
		return s.err
	}

	s.value = value
	return nil
}

func TestScanUsesSQLScanner(t *testing.T) {
	t.Parallel()

	t.Run("nil value", func(t *testing.T) {
		t.Parallel()

		data := &scanner{}
		actual := Optional[*scanner]{Data: data}
		err := actual.Scan(nil)

		require.NoError(t, err)
		require.False(t, actual.Set)
		require.Equal(t, data, actual.Data)
	})

	t.Run("scanned value", func(t *testing.T) {
		t.Parallel()

		actual := Optional[*scanner]{Data: &scanner{}}
		err := actual.Scan("scanned value")

		require.NoError(t, err)
		require.True(t, actual.Set)
		require.Equal(t, "scanned value", actual.Data.value)
	})

	t.Run("scanner error", func(t *testing.T) {
		t.Parallel()

		actual := Set(&scanner{err: errors.New("scanner error")})
		err := actual.Scan("value")

		require.Error(t, err)
	})
}
