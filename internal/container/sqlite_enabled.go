//go:build !postgres_only

package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	sqliteRetrieverRepo "github.com/Tencent/WeKnora/internal/application/repository/retriever/sqlite"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// newSQLiteDatabaseConfig keeps SQLite and its CGO driver out of postgres_only binaries.
func newSQLiteDatabaseConfig() (gorm.Dialector, string, string, error) {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/weknora.db"
	}
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, "", "", fmt.Errorf("failed to create SQLite data directory %s: %w", dir, err)
		}
	}

	sqlite_vec.Auto()
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"
	logger.Infof(context.Background(), "DB Config: driver=sqlite path=%s", dbPath)
	return sqlite.Open(dsn), "sqlite3://" + dbPath, dbPath, nil
}

// registerSQLiteRetrieveEngine wires the optional SQLite retrieval implementation.
func registerSQLiteRetrieveEngine(registry interfaces.RetrieveEngineRegistry, db *gorm.DB) error {
	repo := sqliteRetrieverRepo.NewSQLiteRetrieveEngineRepository(db)
	return registry.Register(retriever.NewKVHybridRetrieveEngine(repo, types.SQLiteRetrieverEngineType))
}

func createSQLiteEngine(_ types.VectorStore, db *gorm.DB) (interfaces.RetrieveEngineService, error) {
	repo := sqliteRetrieverRepo.NewSQLiteRetrieveEngineRepository(db)
	return retriever.NewKVHybridRetrieveEngine(repo, types.SQLiteRetrieverEngineType), nil
}
