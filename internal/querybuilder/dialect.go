package querybuilder

import "fmt"

// PlaceholderDialect generates placeholders for different databases.
type PlaceholderDialect interface {
	Placeholder(index int) string
}

type postgresDialect struct{}

func (postgresDialect) Placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

type sqliteDialect struct{}

func (sqliteDialect) Placeholder(index int) string {
	return "?"
}

// DialectPostgres uses $1, $2, $3, ... placeholders.
var DialectPostgres PlaceholderDialect = postgresDialect{}

// DialectSQLite uses ? for all placeholders.
var DialectSQLite PlaceholderDialect = sqliteDialect{}

// DefaultDialect is the global default placeholder dialect.
var DefaultDialect = DialectPostgres
