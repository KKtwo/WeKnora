//go:build !postgres_only

package database

import (
	"database/sql"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	sqlite3migrate "github.com/golang-migrate/migrate/v4/database/sqlite3"
)

// newSQLiteMigrator isolates the SQLite migration driver from postgres_only binaries.
func newSQLiteMigrator(migrationsPath, dbPath string) (*migrate.Migrate, error) {
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db for migration: %w", err)
	}
	driver, err := sqlite3migrate.WithInstance(sqlDB, &sqlite3migrate.Config{})
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to create sqlite3 migrate driver: %w", err)
	}
	migrator, err := migrate.NewWithDatabaseInstance(migrationsPath, "sqlite3", driver)
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	return migrator, nil
}
