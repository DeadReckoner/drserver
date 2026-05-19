package testutil

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewSqlmockDB returns a *gorm.DB backed by go-sqlmock with the Postgres
// dialect (matching production). The returned Sqlmock lets the test set
// expectations; the cleanup hook closes the underlying connection and asserts
// that all expectations were met.
//
// Use this for handler-layer unit tests where you want to verify the SQL the
// handler emits without standing up a real database.
func NewSqlmockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("gorm.Open: %v", err)
	}

	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
		_ = sqlDB.Close()
	})

	return gormDB, mock
}

// NewSqlmockDBRegex is like NewSqlmockDB but uses regex matching for query
// expectations. Prefer this when query shape (whitespace, identifier
// quoting) makes exact matching brittle.
func NewSqlmockDBRegex(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("gorm.Open: %v", err)
	}

	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
		_ = sqlDB.Close()
	})

	return gormDB, mock
}

// CloseUnderlyingDB closes the *sql.DB behind a *gorm.DB. The standard
// testutil cleanup already handles this for sqlmock fixtures; this helper
// exists for tests that obtain a *gorm.DB through other means.
func CloseUnderlyingDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Logf("CloseUnderlyingDB: %v", err)
		return
	}
	_ = sqlDB.Close()
}

// ensure database/sql is referenced (imported transitively by gorm but kept
// explicit so future helpers in this file can use sql.* without a churn).
var _ = sql.ErrNoRows
