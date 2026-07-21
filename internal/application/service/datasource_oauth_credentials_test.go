package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type oauthConfigRepo struct {
	interfaces.DataSourceRepository
	savedID     string
	savedConfig types.JSON
}

func (r *oauthConfigRepo) UpdateConfig(_ context.Context, id string, config types.JSON) error {
	r.savedID = id
	r.savedConfig = config
	return nil
}

func TestPersistRotatedCredentialsUsesNarrowConfigWrite(t *testing.T) {
	repo := &oauthConfigRepo{}
	service := &DataSourceService{dsRepo: repo}
	config := &types.DataSourceConfig{Credentials: map[string]interface{}{
		"access_token":  "new-access",
		"refresh_token": "new-refresh",
	}}

	err := service.persistRotatedCredentials(
		context.Background(),
		&types.DataSource{ID: "source-1"},
		config,
		[]byte(`{"access_token":"old-access","refresh_token":"old-refresh"}`),
	)

	require.NoError(t, err)
	require.Equal(t, "source-1", repo.savedID)
	require.NotEmpty(t, repo.savedConfig)
}

func TestPersistRotatedCredentialsSkipsUnchangedMap(t *testing.T) {
	repo := &oauthConfigRepo{}
	service := &DataSourceService{dsRepo: repo}
	config := &types.DataSourceConfig{Credentials: map[string]interface{}{"access_token": "same"}}

	err := service.persistRotatedCredentials(
		context.Background(),
		&types.DataSource{ID: "source-1"},
		config,
		credentialSnapshot(config),
	)

	require.NoError(t, err)
	require.Empty(t, repo.savedID)
}
