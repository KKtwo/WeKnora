package docparser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetMaxMessageSizeDefaultsTo101MiB(t *testing.T) {
	t.Setenv("DOCREADER_GRPC_MAX_FILE_SIZE_MB", "")
	t.Setenv("MAX_FILE_SIZE_MB", "50")

	require.Equal(t, 101*1024*1024, getMaxMessageSize())
	require.Greater(t, getMaxMessageSize(), 100*1024*1024)
}

func TestGetMaxMessageSizePrefersDocReaderLimit(t *testing.T) {
	t.Setenv("DOCREADER_GRPC_MAX_FILE_SIZE_MB", "101")
	t.Setenv("MAX_FILE_SIZE_MB", "50")

	require.Equal(t, 101*1024*1024, getMaxMessageSize())
}
