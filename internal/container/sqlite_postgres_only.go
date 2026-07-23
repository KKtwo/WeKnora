//go:build postgres_only

package container

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func newSQLiteDatabaseConfig() (gorm.Dialector, string, string, error) {
	return nil, "", "", fmt.Errorf("SQLite database support is disabled in postgres_only builds")
}

func registerSQLiteRetrieveEngine(interfaces.RetrieveEngineRegistry, *gorm.DB) error {
	return fmt.Errorf("SQLite retrieval support is disabled in postgres_only builds")
}

func createSQLiteEngine(types.VectorStore, *gorm.DB) (interfaces.RetrieveEngineService, error) {
	return nil, fmt.Errorf("SQLite retrieval support is disabled in postgres_only builds")
}
