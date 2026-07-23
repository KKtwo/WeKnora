//go:build postgres_only

package database

import (
	"fmt"

	"github.com/golang-migrate/migrate/v4"
)

func newSQLiteMigrator(string, string) (*migrate.Migrate, error) {
	return nil, fmt.Errorf("SQLite migrations are disabled in postgres_only builds")
}
