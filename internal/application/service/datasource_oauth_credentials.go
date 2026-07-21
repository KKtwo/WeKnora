package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

type dataSourceConfigUpdater interface {
	UpdateConfig(context.Context, string, types.JSON) error
}

func credentialSnapshot(config *types.DataSourceConfig) []byte {
	if config == nil {
		return nil
	}
	snapshot, _ := json.Marshal(config.Credentials)
	return snapshot
}

// persistRotatedCredentials stores refresh-token rotation before the sync run
// completes. Losing a newly issued refresh token would make the next run
// require the user to reconnect.
func (s *DataSourceService) persistRotatedCredentials(
	ctx context.Context,
	ds *types.DataSource,
	config *types.DataSourceConfig,
	before []byte,
) error {
	if ds == nil || config == nil || bytes.Equal(before, credentialSnapshot(config)) {
		return nil
	}
	updater, ok := s.dsRepo.(dataSourceConfigUpdater)
	if !ok {
		return fmt.Errorf("data source repository cannot persist rotated OAuth credentials")
	}
	blob, err := config.ToJSON()
	if err != nil {
		return fmt.Errorf("encrypt rotated OAuth credentials: %w", err)
	}
	if err := updater.UpdateConfig(ctx, ds.ID, blob); err != nil {
		return fmt.Errorf("persist rotated OAuth credentials: %w", err)
	}
	ds.Config = blob
	return nil
}
