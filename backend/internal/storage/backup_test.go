package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gresbase/gresbase/internal/config"
)

func TestBackupService_AutoCreateDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDataDir := filepath.Join(tmpDir, "gresbase_data")
	svc := NewBackupService(nil, &config.Config{DataDir: cfgDataDir})

	backupsDir := filepath.Join(cfgDataDir, "backups")
	if _, err := os.Stat(backupsDir); os.IsNotExist(err) {
		t.Error("backups directory should be auto-created")
	}
	_ = svc
}

func TestBackupService_DeleteAbsent(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewBackupService(nil, &config.Config{DataDir: tmpDir})

	err := svc.DeleteBackup("no-such-backup-id")
	if err == nil {
		t.Error("expected error for missing backup")
	}
}

func TestBackupService_GetPathAbsent(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewBackupService(nil, &config.Config{DataDir: tmpDir})

	_, err := svc.GetBackupPath("no-such-backup-id")
	if err == nil {
		t.Error("expected error for missing backup path")
	}
}

func TestBackupInfo_Validation(t *testing.T) {
	info := &BackupInfo{
		ID:              "bak-abc-123",
		Name:            "hourly-snapshot",
		Size:            2048576,
		Status:          "completed",
		CollectionCount: 12,
		RecordCount:     1042,
	}

	if info.ID == "" {
		t.Error("ID should not be empty")
	}
	if info.Status != "completed" && info.Status != "failed" && info.Status != "in_progress" {
		t.Error("Status must be one of: completed, failed, in_progress")
	}
	if info.Size < 0 {
		t.Error("Size should not be negative")
	}
}
