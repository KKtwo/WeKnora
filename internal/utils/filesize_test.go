package utils

import "testing"

func TestFileSizeDefaultsTo100MiB(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE_MB", "")

	if got, want := GetMaxFileSize(), int64(100*1024*1024); got != want {
		t.Fatalf("GetMaxFileSize() = %d, want %d", got, want)
	}
	if got, want := GetMaxFileSizeMB(), int64(100); got != want {
		t.Fatalf("GetMaxFileSizeMB() = %d, want %d", got, want)
	}
}

func TestFileSizeUsesPositiveEnvironmentOverride(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE_MB", "64")

	if got, want := GetMaxFileSize(), int64(64*1024*1024); got != want {
		t.Fatalf("GetMaxFileSize() = %d, want %d", got, want)
	}
	if got, want := GetMaxFileSizeMB(), int64(64); got != want {
		t.Fatalf("GetMaxFileSizeMB() = %d, want %d", got, want)
	}
}
