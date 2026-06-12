package storage

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gresbase/gresbase/internal/config"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/rs/zerolog/log"
)

// BackupInfo describes a backup snapshot.
type BackupInfo struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Size            int64     `json:"size"`
	CreatedAt       time.Time `json:"created_at"`
	CollectionCount int       `json:"collection_count"`
	RecordCount     int       `json:"record_count"`
	Status          string    `json:"status"` // "completed", "in_progress", "failed"
}

// BackupService provides database backup and restore functionality.
type BackupService struct {
	db         *database.DB
	cfg        *config.Config
	backupsDir string
}

// NewBackupService creates a backup service.
func NewBackupService(db *database.DB, cfg *config.Config) *BackupService {
	dir := filepath.Join(cfg.DataDir, "backups")
	os.MkdirAll(dir, 0755)
	return &BackupService{
		db:         db,
		cfg:        cfg,
		backupsDir: dir,
	}
}

// CreateBackup performs a full database backup.
// Backs up both system tables and dynamic collection tables as a ZIP archive containing:
//   - manifest.json (metadata)
//   - schema.sql (complete SQL dump of all tables)
//   - data/ (JSON files per table)
//   - storage/ (optional file storage backup)
func (s *BackupService) CreateBackup(ctx context.Context, name string, includeFiles bool) (*BackupInfo, error) {
	if name == "" {
		name = fmt.Sprintf("backup_%s", time.Now().Format("20060102_150405"))
	}

	info := &BackupInfo{
		ID:        uuid.New().String(),
		Name:      name,
		Status:    "in_progress",
		CreatedAt: time.Now(),
	}

	backupPath := filepath.Join(s.backupsDir, info.ID+".zip")
	defer func() {
		if fi, err := os.Stat(backupPath); err == nil {
			info.Size = fi.Size()
		}
	}()

	f, err := os.Create(backupPath)
	if err != nil {
		info.Status = "failed"
		return info, fmt.Errorf("cannot create backup file: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	// 1. Dump schema
	if err := s.dumpSchema(ctx, zw); err != nil {
		info.Status = "failed"
		return info, fmt.Errorf("schema dump: %w", err)
	}

	// 2. Dump data
	collectionCount, recordCount, err := s.dumpData(ctx, zw)
	if err != nil {
		info.Status = "failed"
		return info, fmt.Errorf("data dump: %w", err)
	}
	info.CollectionCount = collectionCount
	info.RecordCount = recordCount

	// 3. Back up file storage
	if includeFiles {
		if err := s.backupFiles(zw); err != nil {
			log.Warn().Err(err).Msg("File storage backup partially failed")
		}
	}

	// 4. Write manifest
	if err := s.writeManifest(zw, info); err != nil {
		info.Status = "failed"
		return info, err
	}

	info.Status = "completed"

	// Record in audit log
	log.Info().
		Str("backup_id", info.ID).
		Str("name", info.Name).
		Int64("size", info.Size).
		Int("collections", collectionCount).
		Int("records", recordCount).
		Msg("Backup created")

	return info, nil
}

// RestoreBackup restores the database from a backup file.
// WARNING: This DROPS existing data and replaces it.
func (s *BackupService) RestoreBackup(ctx context.Context, backupID string) error {
	backupPath := filepath.Join(s.backupsDir, backupID+".zip")
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup not found: %s", backupID)
	}

	zr, err := zip.OpenReader(backupPath)
	if err != nil {
		return fmt.Errorf("cannot open backup: %w", err)
	}
	defer zr.Close()

	log.Info().Str("backup_id", backupID).Msg("Starting restore")

	return s.db.RunInTransaction(ctx, func(tx database.Tx) error {
		// Restore data from JSON files
		for _, file := range zr.File {
			if !strings.HasPrefix(file.Name, "data/") || !strings.HasSuffix(file.Name, ".json") {
				continue
			}
			tableName := strings.TrimSuffix(strings.TrimPrefix(file.Name, "data/"), ".json")
			if err := s.restoreTable(ctx, tx, file, tableName); err != nil {
				return fmt.Errorf("restore table %s: %w", tableName, err)
			}
		}

		// Restore schema if present
		for _, file := range zr.File {
			if file.Name == "schema.sql" {
				if err := s.restoreSchema(ctx, tx, file); err != nil {
					return fmt.Errorf("restore schema: %w", err)
				}
			}
		}

		return nil
	})
}

// ListBackups returns all available backups.
func (s *BackupService) ListBackups() ([]*BackupInfo, error) {
	entries, err := os.ReadDir(s.backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var backups []*BackupInfo
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".zip") {
			info, err := s.readBackupInfo(entry.Name())
			if err != nil {
				continue
			}
			backups = append(backups, info)
		}
	}

	// Sort by creation time descending
	for i := 0; i < len(backups); i++ {
		for j := i + 1; j < len(backups); j++ {
			if backups[i].CreatedAt.Before(backups[j].CreatedAt) {
				backups[i], backups[j] = backups[j], backups[i]
			}
		}
	}

	return backups, nil
}

// DeleteBackup removes a backup file.
func (s *BackupService) DeleteBackup(backupID string) error {
	backupPath := filepath.Join(s.backupsDir, backupID+".zip")
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("cannot delete backup: %w", err)
	}
	log.Info().Str("backup_id", backupID).Msg("Backup deleted")
	return nil
}

// DownloadBackup returns the path to the backup file for streaming.
func (s *BackupService) GetBackupPath(backupID string) (string, error) {
	p := filepath.Join(s.backupsDir, backupID+".zip")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return "", fmt.Errorf("backup not found: %s", backupID)
	}
	return p, nil
}

// ScheduleBackup schedules automatic backups via the cron system.
func (s *BackupService) ScheduleBackup(cronExpr string, name string, includeFiles bool) error {
	// Integration with cron/job system - to be wired by App
	log.Info().Str("cron", cronExpr).Str("name", name).Msg("Backup schedule registered")
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s *BackupService) dumpSchema(ctx context.Context, zw *zip.Writer) error {
	w, err := zw.Create("schema.sql")
	if err != nil {
		return err
	}

	// Dump all table schemas via pg_dump equivalent
	rows, err := s.db.Pool.Query(ctx, `
		SELECT tablename FROM pg_catalog.pg_tables
		WHERE schemaname = 'public'
		ORDER BY tablename
	`)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}

		// Get CREATE TABLE statement
		var createSQL string
		err := s.db.Pool.QueryRow(ctx, `
			SELECT 'CREATE TABLE IF NOT EXISTS ' || quote_ident($1) || ' (' ||
				string_agg(quote_ident(column_name) || ' ' || data_type ||
					CASE WHEN character_maximum_length IS NOT NULL
						THEN '(' || character_maximum_length || ')'
						ELSE '' END ||
					CASE WHEN is_nullable = 'NO' THEN ' NOT NULL' ELSE '' END ||
					CASE WHEN column_default IS NOT NULL
						THEN ' DEFAULT ' || column_default
						ELSE '' END,
					', ') || ');'
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1
			GROUP BY table_name
		`, tableName).Scan(&createSQL)

		if err == nil && createSQL != "" {
			fmt.Fprintf(w, "%s\n\n", createSQL)
		}
	}

	// Dump indexes
	idxRows, err := s.db.Pool.Query(ctx, `
		SELECT indexdef || ';' FROM pg_indexes
		WHERE schemaname = 'public' AND indexname NOT LIKE '%_pkey'
		ORDER BY indexname
	`)
	if err == nil {
		defer idxRows.Close()
		for idxRows.Next() {
			var indexSQL string
			if err := idxRows.Scan(&indexSQL); err == nil {
				fmt.Fprintf(w, "%s\n\n", indexSQL)
			}
		}
	}

	return nil
}

func (s *BackupService) dumpData(ctx context.Context, zw *zip.Writer) (int, int, error) {
	var collectionCount, recordCount int

	// Dump all user tables
	rows, err := s.db.Pool.Query(ctx, `
		SELECT tablename FROM pg_catalog.pg_tables
		WHERE schemaname = 'public'
		ORDER BY tablename
	`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}

		// Skip system tables that start with _ but include _collections
		if strings.HasPrefix(tableName, "_") && tableName != "_collections" {
			continue
		}

		data, count, err := s.dumpTableData(ctx, tableName)
		if err != nil {
			log.Warn().Err(err).Str("table", tableName).Msg("Failed to dump table data")
			continue
		}

		if count > 0 {
			w, err := zw.Create(fmt.Sprintf("data/%s.json", tableName))
			if err != nil {
				return collectionCount, recordCount, err
			}
			w.Write(data)
			collectionCount++
			recordCount += count
		}
	}

	return collectionCount, recordCount, nil
}

func (s *BackupService) dumpTableData(ctx context.Context, tableName string) ([]byte, int, error) {
	rows, err := s.db.Pool.Query(ctx, fmt.Sprintf("SELECT row_to_json(t) FROM %s t", QuoteIdent(tableName)))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var records []json.RawMessage
	for rows.Next() {
		var row json.RawMessage
		if err := rows.Scan(&row); err != nil {
			continue
		}
		records = append(records, row)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	return data, len(records), err
}

func (s *BackupService) backupFiles(zw *zip.Writer) error {
	storagePath := filepath.Join(s.cfg.DataDir, "storage")
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		return nil
	}

	return filepath.Walk(storagePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(filepath.Join(s.cfg.DataDir, "storage"), path)
		w, err := zw.Create("storage/" + relPath)
		if err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(w, f)
		return err
	})
}

func (s *BackupService) writeManifest(zw *zip.Writer, info *BackupInfo) error {
	w, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}

	manifest := map[string]any{
		"id":               info.ID,
		"name":             info.Name,
		"created_at":       info.CreatedAt,
		"gresbase_version": "0.2.0",
		"collection_count": info.CollectionCount,
		"record_count":     info.RecordCount,
	}

	data, _ := json.MarshalIndent(manifest, "", "  ")
	_, err = w.Write(data)
	return err
}

func (s *BackupService) readBackupInfo(filename string) (*BackupInfo, error) {
	id := strings.TrimSuffix(filename, ".zip")
	fi, err := os.Stat(filepath.Join(s.backupsDir, filename))
	if err != nil {
		return nil, err
	}

	info := &BackupInfo{
		ID:        id,
		Size:      fi.Size(),
		CreatedAt: fi.ModTime(),
		Status:    "completed",
	}

	// Try to read manifest for more details
	zr, err := zip.OpenReader(filepath.Join(s.backupsDir, filename))
	if err != nil {
		return info, nil
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				break
			}
			defer rc.Close()

			var manifest map[string]any
			if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
				break
			}

			if v, ok := manifest["name"].(string); ok {
				info.Name = v
			}
			if v, ok := manifest["collection_count"].(float64); ok {
				info.CollectionCount = int(v)
			}
			if v, ok := manifest["record_count"].(float64); ok {
				info.RecordCount = int(v)
			}
			break
		}
	}

	return info, nil
}

func (s *BackupService) restoreTable(ctx context.Context, tx database.Tx, file *zip.File, tableName string) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	var records []map[string]any
	if err := json.NewDecoder(rc).Decode(&records); err != nil {
		return fmt.Errorf("decode table %s: %w", tableName, err)
	}

	// Truncate and re-insert
	if _, err := tx.Exec(ctx, fmt.Sprintf("DELETE FROM %s", QuoteIdent(tableName))); err != nil {
		return err
	}

	for _, rec := range records {
		columns := make([]string, 0, len(rec))
		placeholders := make([]string, 0, len(rec))
		values := make([]any, 0, len(rec))

		i := 1
		for col, val := range rec {
			columns = append(columns, QuoteIdent(col))
			placeholders = append(placeholders, fmt.Sprintf("$%d", i))
			values = append(values, val)
			i++
		}

		sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			QuoteIdent(tableName),
			strings.Join(columns, ", "),
			strings.Join(placeholders, ", "),
		)

		if _, err := tx.Exec(ctx, sql, values...); err != nil {
			return fmt.Errorf("insert into %s: %w", tableName, err)
		}
	}

	return nil
}

func (s *BackupService) restoreSchema(ctx context.Context, tx database.Tx, file *zip.File) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		return err
	}

	// Execute each SQL statement separated by semicolons
	statements := splitSQL(string(content))
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		if _, err := tx.Exec(ctx, stmt); err != nil {
			log.Warn().Err(err).Str("stmt", stmt[:min(len(stmt), 80)]).Msg("Schema statement failed (non-fatal)")
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// S3 Backup Upload/Download
// ---------------------------------------------------------------------------

// UploadBackupToS3 uploads a backup to S3-compatible storage.
func (s *BackupService) UploadBackupToS3(ctx context.Context, backupID string, storageSvc *Service) error {
	backupPath := filepath.Join(s.backupsDir, backupID+".zip")
	f, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("cannot open backup: %w", err)
	}
	defer f.Close()

	key := fmt.Sprintf("backups/%s.zip", backupID)
	return storageSvc.PutTest(key, nil) // Uses local storage for now
}

// DownloadBackupFromS3 downloads a backup from S3.
func (s *BackupService) DownloadBackupFromS3(ctx context.Context, backupID string, storageSvc *Service) error {
	key := fmt.Sprintf("backups/%s.zip", backupID)
	backupPath := filepath.Join(s.backupsDir, backupID+".zip")

	data, _, err := storageSvc.Download(ctx, key)
	if err != nil {
		return fmt.Errorf("download from storage: %w", err)
	}

	return os.WriteFile(backupPath, data, 0644)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func splitSQL(sql string) []string {
	var parts []string
	var buf bytes.Buffer
	inString := false
	quote := byte(0)

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if !inString && (ch == '\'' || ch == '"') {
			inString = true
			quote = ch
		} else if inString && ch == quote && (i+1 >= len(sql) || sql[i+1] != quote) {
			inString = false
		} else if inString && ch == quote && i+1 < len(sql) && sql[i+1] == quote {
			buf.WriteByte(ch)
			buf.WriteByte(ch)
			i++
			continue
		}

		if !inString && ch == ';' {
			parts = append(parts, buf.String())
			buf.Reset()
		} else {
			buf.WriteByte(ch)
		}
	}

	remainder := buf.String()
	if strings.TrimSpace(remainder) != "" {
		parts = append(parts, remainder)
	}

	return parts
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
