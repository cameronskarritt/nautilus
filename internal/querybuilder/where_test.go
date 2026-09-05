package querybuilder

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestEq(t *testing.T) {
	t.Parallel()

	t.Run("single condition", func(t *testing.T) {
		t.Parallel()
		conds := Eq{"id": 1}.Conds()
		require.Len(t, conds, 1)
		require.Equal(t, "id", conds[0].column)
		require.Equal(t, OpEq, conds[0].op)
		require.Equal(t, 1, conds[0].value)
		require.False(t, conds[0].isNull)
	})

	t.Run("nil value becomes IS NULL", func(t *testing.T) {
		t.Parallel()
		conds := Eq{"deleted_at": nil}.Conds()
		require.Len(t, conds, 1)
		require.Equal(t, "deleted_at", conds[0].column)
		require.True(t, conds[0].isNull)
	})

	t.Run("multiple conditions sorted by key", func(t *testing.T) {
		t.Parallel()
		conds := Eq{
			"org_id": 2,
			"id":     1,
			"status": "active",
		}.Conds()
		require.Len(t, conds, 3)
		// Should be sorted: id, org_id, status
		require.Equal(t, "id", conds[0].column)
		require.Equal(t, "org_id", conds[1].column)
		require.Equal(t, "status", conds[2].column)
	})

	t.Run("InList value becomes IN", func(t *testing.T) {
		t.Parallel()
		conds := Eq{"id": In(1, 2, 3)}.Conds()
		require.Len(t, conds, 1)
		require.Equal(t, "id", conds[0].column)
		require.Equal(t, OpIn, conds[0].op)
		require.Equal(t, []any{1, 2, 3}, conds[0].value)
		require.False(t, conds[0].isNull)
	})
}

func TestComparisonOperators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name    string
		Builder func() Cond
		Op      Op
		Column  string
		Value   any
		IsNull  bool
	}{
		{
			Name:    "Ne",
			Builder: func() Cond { return Ne("status", "deleted") },
			Op:      OpNe,
			Column:  "status",
			Value:   "deleted",
		},
		{
			Name:    "Gt",
			Builder: func() Cond { return Gt("age", 18) },
			Op:      OpGt,
			Column:  "age",
			Value:   18,
		},
		{
			Name:    "Gte",
			Builder: func() Cond { return Gte("score", 100) },
			Op:      OpGte,
			Column:  "score",
			Value:   100,
		},
		{
			Name:    "Lt",
			Builder: func() Cond { return Lt("count", 10) },
			Op:      OpLt,
			Column:  "count",
			Value:   10,
		},
		{
			Name:    "Lte",
			Builder: func() Cond { return Lte("balance", 0) },
			Op:      OpLte,
			Column:  "balance",
			Value:   0,
		},
		{
			Name:    "Like",
			Builder: func() Cond { return Like("email", "%@example.com") },
			Op:      OpLike,
			Column:  "email",
			Value:   "%@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			cond := tt.Builder()
			require.Equal(t, tt.Op, cond.op)
			require.Equal(t, tt.Column, cond.column)
			require.Equal(t, tt.Value, cond.value)
			require.Equal(t, tt.IsNull, cond.isNull)
		})
	}
}
