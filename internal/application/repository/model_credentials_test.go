package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModelRepositoryUpdateEncryptsLegacyPlaintextCredentials(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Model{}))

	model := &types.Model{
		ID:       "embedding-model",
		TenantID: 10001,
		Name:     "embedding",
		Type:     types.ModelTypeEmbedding,
		Source:   types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			APIKey: "legacy-plaintext-secret",
		},
	}
	t.Setenv("SYSTEM_AES_KEY", "")
	require.NoError(t, db.Create(model).Error)

	// Updating a legacy row after encryption is enabled must migrate the
	// replacement credential to ciphertext in the same write.
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	model.Parameters.APIKey = "replacement-secret"
	require.NoError(t, NewModelRepository(db).Update(context.Background(), model))

	var raw string
	require.NoError(t, db.Raw(
		"SELECT parameters FROM models WHERE id = ?", model.ID,
	).Scan(&raw).Error)
	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	apiKey, ok := stored["api_key"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(apiKey, "enc:v1:"), "API key must be encrypted at rest")
}
