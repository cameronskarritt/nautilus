package pagination

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/testutil/require"
)

func TestEncodeDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name   string
		Cursor Cursor
	}{
		{
			Name:   "single value",
			Cursor: Cursor{"id": "123"},
		},
		{
			Name: "composite value",
			Cursor: Cursor{
				"created_at": "2024-01-01T00:00:00Z",
				"id":         "123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			decoded, err := Decode(Encode(tt.Cursor))
			require.NoError(t, err)
			require.Equal(t, tt.Cursor, decoded)
		})
	}

	t.Run("numeric value decodes as float64", func(t *testing.T) {
		t.Parallel()

		decoded, err := Decode(Encode(Cursor{"id": 123}))
		require.NoError(t, err)
		require.Equal(t, float64(123), decoded["id"])
	})
}

func TestEncodeEmptyCursor(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", Encode(nil))
	require.Equal(t, "", Encode(Cursor{}))
}

func TestDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Input         string
		Expected      Cursor
		ExpectedError bool
	}{
		{Name: "empty string", Input: "", Expected: nil},
		{Name: "valid cursor", Input: "eyJpZCI6IjEyMyJ9", Expected: Cursor{"id": "123"}},
		{Name: "invalid base64", Input: "not-base64!!!", ExpectedError: true},
		{Name: "invalid json", Input: "bm90LWpzb24", ExpectedError: true},
		{Name: "padded base64", Input: "dGVzdA==", ExpectedError: true},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			actual, err := Decode(tt.Input)
			if tt.ExpectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, actual)
		})
	}
}

func TestParseParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Query         string
		MaxLimit      int
		Expected      Params
		ExpectedError bool
	}{
		{
			Name:     "default limit",
			MaxLimit: 50,
			Expected: Params{
				Limit: 50,
			},
		},
		{
			Name:     "limit within max",
			Query:    "limit=20",
			MaxLimit: 50,
			Expected: Params{
				Limit: 20,
			},
		},
		{
			Name:     "limit above max is capped",
			Query:    "limit=100",
			MaxLimit: 50,
			Expected: Params{
				Limit: 50,
			},
		},
		{
			Name:     "non-positive limit uses max",
			Query:    "limit=0",
			MaxLimit: 50,
			Expected: Params{
				Limit: 50,
			},
		},
		{
			Name:     "invalid limit uses max",
			Query:    "limit=abc",
			MaxLimit: 50,
			Expected: Params{
				Limit: 50,
			},
		},
		{
			Name:     "cursor",
			Query:    "cursor=eyJpZCI6IjEyMyJ9",
			MaxLimit: 50,
			Expected: Params{
				Limit:  50,
				Cursor: Cursor{"id": "123"},
			},
		},
		{
			Name:     "limit and cursor",
			Query:    "limit=20&cursor=eyJpZCI6IjEyMyJ9",
			MaxLimit: 50,
			Expected: Params{
				Limit:  20,
				Cursor: Cursor{"id": "123"},
			},
		},
		{
			Name:          "invalid cursor",
			Query:         "cursor=invalid!!!",
			MaxLimit:      50,
			ExpectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/?"+tt.Query, nil)
			actual, err := ParseParams(req, tt.MaxLimit)
			if tt.ExpectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, actual)
		})
	}
}

func TestParseParamsDefaultLimit(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/?limit=100", nil)
	actual, err := ParseParams(req)

	require.NoError(t, err)
	require.Equal(t, Params{Limit: DefaultLimit}, actual)
}

func TestBuild(t *testing.T) {
	t.Parallel()

	type item struct {
		ID        string
		CreatedAt string
	}

	tests := []struct {
		Name     string
		Items    []item
		Limit    int
		CursorFn CursorFunc[item]
		Expected Page[item]
	}{
		{
			Name:  "empty items",
			Items: []item{},
			Limit: 10,
			CursorFn: func(i item) Cursor {
				return Cursor{"id": i.ID}
			},
			Expected: Page[item]{Data: []item{}},
		},
		{
			Name: "exactly limit items",
			Items: []item{
				{ID: "1"},
				{ID: "2"},
				{ID: "3"},
			},
			Limit: 3,
			CursorFn: func(i item) Cursor {
				return Cursor{"id": i.ID}
			},
			Expected: Page[item]{
				Data: []item{
					{ID: "1"},
					{ID: "2"},
					{ID: "3"},
				},
			},
		},
		{
			Name: "limit plus one item",
			Items: []item{
				{ID: "1"},
				{ID: "2"},
				{ID: "3"},
				{ID: "4"},
			},
			Limit: 3,
			CursorFn: func(i item) Cursor {
				return Cursor{"id": i.ID}
			},
			Expected: Page[item]{
				Data: []item{
					{ID: "1"},
					{ID: "2"},
					{ID: "3"},
				},
				HasMore:    true,
				NextCursor: Encode(Cursor{"id": "3"}),
			},
		},
		{
			Name: "composite cursor",
			Items: []item{
				{ID: "1", CreatedAt: "2024-01-01"},
				{ID: "2", CreatedAt: "2024-01-02"},
				{ID: "3", CreatedAt: "2024-01-03"},
				{ID: "4", CreatedAt: "2024-01-04"},
			},
			Limit: 3,
			CursorFn: func(i item) Cursor {
				return Cursor{"created_at": i.CreatedAt, "id": i.ID}
			},
			Expected: Page[item]{
				Data: []item{
					{ID: "1", CreatedAt: "2024-01-01"},
					{ID: "2", CreatedAt: "2024-01-02"},
					{ID: "3", CreatedAt: "2024-01-03"},
				},
				HasMore:    true,
				NextCursor: Encode(Cursor{"created_at": "2024-01-03", "id": "3"}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, Build(tt.Items, tt.Limit, tt.CursorFn))
		})
	}
}
