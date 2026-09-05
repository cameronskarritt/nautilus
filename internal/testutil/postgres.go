package testutil

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/errors"
)

const (
	testDBName     = "nautilus_test"
	templateDBName = "nautilus_template"
	testUser       = "testuser"
	testPassword   = "testpass"
)

var (
	templateOnce sync.Once
	templateErr  error
)

// initializeTemplate creates and initializes the template database once.
func initializeTemplate() error {
	templateOnce.Do(func() {
		ctx := context.Background()

		// Connect to the default test database
		db, err := postgres.Connect(ctx, baseConnStr)
		if err != nil {
			templateErr = errors.Wrap(err, "failed to connect to test db")
			return
		}

		// Create the template database
		_, err = db.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", templateDBName))
		if err != nil {
			templateErr = errors.Wrap(err, "failed to drop template db")
			db.Close()
			return
		}

		_, err = db.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", templateDBName))
		if err != nil {
			templateErr = errors.Wrap(err, "failed to create template db")
			db.Close()
			return
		}
		db.Close()

		// Connect to template database and initialize schema
		templateConnStr := replaceDBName(baseConnStr, templateDBName)
		templateDB, err := postgres.Connect(ctx, templateConnStr)
		if err != nil {
			templateErr = errors.Wrap(err, "failed to connect to template db")
			return
		}
		defer templateDB.Close()

		var migrator postgres.Migrator
		err = database.Initialize(ctx, templateDB, migrator)
		if err != nil {
			templateErr = errors.Wrap(err, "failed to initialize template db")
			return
		}

		err = database.Migrate(ctx, templateDB, migrator)
		if err != nil {
			templateErr = errors.Wrap(err, "failed to migrate template db")
			return
		}
	})

	return templateErr
}

// SetupTestDB returns a database connection wrapped in a transaction that
// automatically rolls back when the test completes. This is the fastest
// approach for most tests.
//
// Note: Nested transactions (when your code calls Begin()) will use savepoints.
// This means you cannot test actual commit behavior with this helper.
func SetupTestDB(t *testing.T) database.Database {
	t.Helper()

	if err := startContainer(); err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	if err := initializeTemplate(); err != nil {
		t.Fatalf("failed to initialize template database: %v", err)
	}

	ctx := context.Background()

	// Connect to template database
	templateConnStr := replaceDBName(baseConnStr, templateDBName)
	db, err := postgres.Connect(ctx, templateConnStr)
	if err != nil {
		t.Fatalf("failed to connect to template db: %v", err)
	}

	// Begin transaction
	tx, err := db.Begin(ctx)
	if err != nil {
		db.Close()
		t.Fatalf("failed to begin transaction: %v", err)
	}

	// Register cleanup to rollback and close connection
	t.Cleanup(func() {
		_ = tx.Rollback(ctx)
		db.Close()
	})

	return tx
}

// SetupTestDBWithCommit returns a database connection to a fresh database
// cloned from the template. Use this when you need to test actual commit
// behavior or transaction semantics.
//
// This is slower than SetupTestDB as it creates and drops a database per test.
func SetupTestDBWithCommit(t *testing.T) database.Database {
	t.Helper()

	if err := startContainer(); err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	if err := initializeTemplate(); err != nil {
		t.Fatalf("failed to initialize template database: %v", err)
	}

	ctx := context.Background()

	// Generate unique database name for this test
	testDBName := testDatabaseName(t.Name())

	// Connect to default database to create the test database
	adminDB, err := postgres.Connect(ctx, baseConnStr)
	if err != nil {
		t.Fatalf("failed to connect to admin db: %v", err)
	}

	// Create database from template
	_, err = adminDB.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", testDBName, templateDBName))
	if err != nil {
		adminDB.Close()
		t.Fatalf("failed to create test db from template: %v", err)
	}
	adminDB.Close()

	// Connect to the new test database
	testConnStr := replaceDBName(baseConnStr, testDBName)
	db, err := postgres.Connect(ctx, testConnStr)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}

	// Register cleanup to close connection and drop database
	t.Cleanup(func() {
		db.Close()

		// Reconnect to admin to drop the test database
		cleanupCtx := context.Background()
		cleanupDB, err := postgres.Connect(cleanupCtx, baseConnStr)
		if err != nil {
			t.Logf("warning: failed to connect for cleanup: %v", err)
			return
		}
		defer cleanupDB.Close()

		// Terminate any remaining connections to the test database
		_, _ = cleanupDB.Exec(cleanupCtx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'",
			testDBName,
		))

		_, err = cleanupDB.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", testDBName))
		if err != nil {
			t.Logf("warning: failed to drop test db: %v", err)
		}
	})

	return db
}

// SetupEmptyTestDB returns a connection to a fresh database with no schema.
func SetupEmptyTestDB(t *testing.T) database.Database {
	t.Helper()

	if err := startContainer(); err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	ctx := context.Background()
	testDBName := testDatabaseName(t.Name())

	adminDB, err := postgres.Connect(ctx, baseConnStr)
	if err != nil {
		t.Fatalf("failed to connect to admin db: %v", err)
	}

	_, err = adminDB.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", testDBName))
	if err != nil {
		adminDB.Close()
		t.Fatalf("failed to create test db: %v", err)
	}
	adminDB.Close()

	testConnStr := replaceDBName(baseConnStr, testDBName)
	db, err := postgres.Connect(ctx, testConnStr)
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}

	t.Cleanup(func() {
		db.Close()

		cleanupCtx := context.Background()
		cleanupDB, err := postgres.Connect(cleanupCtx, baseConnStr)
		if err != nil {
			t.Logf("warning: failed to connect for cleanup: %v", err)
			return
		}
		defer cleanupDB.Close()

		_, _ = cleanupDB.Exec(cleanupCtx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'",
			testDBName,
		))

		_, err = cleanupDB.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", testDBName))
		if err != nil {
			t.Logf("warning: failed to drop test db: %v", err)
		}
	})

	return db
}

func testDatabaseName(name string) string {
	suffix := fmt.Sprintf("_%d", time.Now().UnixNano())
	prefix := "test_" + sanitizeName(name)
	maxPrefixLen := 63 - len(suffix)
	if len(prefix) > maxPrefixLen {
		prefix = prefix[:maxPrefixLen]
	}
	return prefix + suffix
}

// replaceDBName replaces the database name in a connection string.
func replaceDBName(connStr, dbName string) string {
	// Connection string format: postgres://user:pass@host:port/dbname?params
	// We need to replace the database name portion
	return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		testUser, testPassword, getHostPort(connStr), dbName)
}

// getHostPort extracts the host:port from a connection string.
func getHostPort(connStr string) string {
	// Simple extraction - assumes format postgres://user:pass@host:port/dbname
	// Find the @ and the next /
	start := 0
	for i := 0; i < len(connStr); i++ {
		if connStr[i] == '@' {
			start = i + 1
			break
		}
	}
	end := len(connStr)
	for i := start; i < len(connStr); i++ {
		if connStr[i] == '/' {
			end = i
			break
		}
	}
	return connStr[start:end]
}

// sanitizeName converts a test name to a valid PostgreSQL identifier.
func sanitizeName(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			result = append(result, c+'a'-'A')
		} else if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	// Truncate to reasonable length for postgres identifier
	if len(result) > 40 {
		result = result[:40]
	}
	return string(result)
}
