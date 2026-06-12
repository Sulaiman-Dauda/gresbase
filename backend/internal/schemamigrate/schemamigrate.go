// Package schemamigrate implements schema migration files
// for USER collections, so collection schemas live in git and deploy
// reproducibly. It is entirely separate from Gresbase's own internal
// migrations (backend/migrations): those manage the platform's system tables,
// this package manages the dynamic collections created by users.
//
// File format (JSON, one file per change, applied in lexicographic order):
//
//	{
//	  "formatVersion": 1,
//	  "name": "create articles",
//	  "createdAt": "2026-06-12T10:00:00Z",
//	  "collections": [ { ...full collection snapshot... } ],
//	  "deleted": [ "old_collection_name" ]
//	}
//
// Semantics are declarative full-snapshots: applying a file upserts every
// listed collection to exactly that definition (create if missing, else
// update — the collection service ALTERs the underlying table), then deletes
// the collections listed in "deleted" if they still exist. Apply is
// idempotent by construction. Collection snapshots use the same JSON shape as
// the collections export/import API, so files are hand-editable.
//
// Files live in ./gb_migrations/ by default (working-directory relative, like
// gb_hooks), named {unix_timestamp}_{slug}.json. Applied files are tracked in
// the _collection_migrations table, created lazily by this package.
//
// Git semantics: two files touching the same collection apply in filename
// (timestamp) order — last write wins. Merge conflicts across branches are
// therefore resolved by snapshot ordering, not by field-level merging; after
// merging branches that both changed a collection, the newest snapshot is the
// effective one. Run `gresbase migrations snapshot` to re-baseline if needed.
package schemamigrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/jackc/pgx/v5"
)

// FormatVersion is the migration file format understood by this build.
const FormatVersion = 1

// TrackingTable records applied migration filenames.
const TrackingTable = "_collection_migrations"

// MigrationFile is the on-disk migration format. Collections are full
// *collection.Collection snapshots, which keeps the locked-by-default
// tri-state access rules intact: a *string rule marshals to JSON null
// (locked), "" (public), or an expression string.
type MigrationFile struct {
	FormatVersion int                      `json:"formatVersion"`
	Name          string                   `json:"name"`
	CreatedAt     time.Time                `json:"createdAt"`
	Collections   []*collection.Collection `json:"collections"`
	Deleted       []string                 `json:"deleted,omitempty"`
}

// Status describes one migration file for listing.
type Status struct {
	Filename  string
	Applied   bool
	AppliedAt time.Time
}

// Runner applies and records collection schema migrations.
type Runner struct {
	db  *database.DB
	svc *collection.Service
	dir string

	appliedThisRun []string
}

// New creates a Runner over the given migrations directory.
func New(db *database.DB, svc *collection.Service, dir string) *Runner {
	return &Runner{db: db, svc: svc, dir: dir}
}

// Dir returns the migrations directory.
func (r *Runner) Dir() string { return r.dir }

// AppliedThisRun returns the filenames applied by this Runner during the
// current process (e.g. during Bootstrap), in application order.
func (r *Runner) AppliedThisRun() []string { return r.appliedThisRun }

// EnsureTable lazily creates the tracking table.
func (r *Runner) EnsureTable(ctx context.Context) error {
	return r.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS `+TrackingTable+` (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
}

// Apply scans the migrations directory and applies every unapplied file in
// lexicographic order. Each file runs in its own transaction together with
// its tracking row. A failing file aborts with an error naming the file —
// fail-closed, since a half-applied schema is worse than not starting.
// A missing directory is a no-op.
func (r *Runner) Apply(ctx context.Context) ([]string, error) {
	if r.dir == "" {
		return nil, nil
	}
	files, err := r.scanDir()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	if err := r.EnsureTable(ctx); err != nil {
		return nil, fmt.Errorf("ensure %s table: %w", TrackingTable, err)
	}
	appliedSet, err := r.appliedSet(ctx)
	if err != nil {
		return nil, err
	}

	var applied []string
	for _, name := range files {
		if _, done := appliedSet[name]; done {
			continue
		}
		if err := r.applyFile(ctx, name); err != nil {
			return applied, fmt.Errorf("collection schema migration %q: %w", name, err)
		}
		applied = append(applied, name)
		r.appliedThisRun = append(r.appliedThisRun, name)
	}
	return applied, nil
}

// List returns every migration file in the directory with its applied state.
func (r *Runner) List(ctx context.Context) ([]Status, error) {
	files, err := r.scanDir()
	if err != nil {
		return nil, err
	}
	if err := r.EnsureTable(ctx); err != nil {
		return nil, err
	}

	appliedAt := map[string]time.Time{}
	rows, err := r.db.Query(ctx, `SELECT filename, applied_at FROM `+TrackingTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var at time.Time
		if err := rows.Scan(&name, &at); err != nil {
			return nil, err
		}
		appliedAt[name] = at
	}

	statuses := make([]Status, 0, len(files))
	for _, name := range files {
		at, ok := appliedAt[name]
		statuses = append(statuses, Status{Filename: name, Applied: ok, AppliedAt: at})
	}
	return statuses, nil
}

// Snapshot writes a single migration file containing the full current
// snapshot of all non-system collections (no deletions), marked as already
// applied. This is how an existing instance bootstraps its migration history.
func (r *Runner) Snapshot(ctx context.Context, name string) (string, error) {
	colls, err := r.svc.ListCollections(ctx)
	if err != nil {
		return "", err
	}
	mf := BuildSnapshot(colls, name)
	if len(mf.Collections) == 0 {
		return "", errors.New("no non-system collections to snapshot")
	}
	return r.writeApplied(ctx, mf, Slugify(name))
}

// BuildSnapshot assembles a snapshot migration file from collections,
// excluding system collections.
func BuildSnapshot(colls []*collection.Collection, name string) *MigrationFile {
	if name == "" {
		name = "snapshot"
	}
	mf := &MigrationFile{
		FormatVersion: FormatVersion,
		Name:          name,
		CreatedAt:     time.Now().UTC(),
	}
	for _, coll := range colls {
		if coll == nil || coll.System {
			continue
		}
		mf.Collections = append(mf.Collections, coll)
	}
	return mf
}

// RecordChange writes an automigrate file for a created/updated collection:
// a full snapshot of that collection, pre-marked as applied (the live
// database already has this state). System collections are skipped.
// Returns the filename, or "" when skipped.
func (r *Runner) RecordChange(ctx context.Context, action string, coll *collection.Collection) (string, error) {
	if coll == nil || coll.System {
		return "", nil
	}
	mf := &MigrationFile{
		FormatVersion: FormatVersion,
		Name:          action + " " + coll.Name,
		CreatedAt:     time.Now().UTC(),
		Collections:   []*collection.Collection{coll},
	}
	return r.writeApplied(ctx, mf, Slugify(action+"_"+coll.Name))
}

// RecordDelete writes an automigrate file recording a collection deletion,
// pre-marked as applied. Underscore-prefixed (system) names are skipped.
// Returns the filename, or "" when skipped.
func (r *Runner) RecordDelete(ctx context.Context, name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "_") {
		return "", nil
	}
	mf := &MigrationFile{
		FormatVersion: FormatVersion,
		Name:          "delete " + name,
		CreatedAt:     time.Now().UTC(),
		Deleted:       []string{name},
	}
	return r.writeApplied(ctx, mf, Slugify("delete_"+name))
}

// ---------------------------------------------------------------------------
// Internals
// ---------------------------------------------------------------------------

func (r *Runner) scanDir() ([]string, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read migrations dir %q: %w", r.dir, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	return files, nil
}

func (r *Runner) appliedSet(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.db.Query(ctx, `SELECT filename FROM `+TrackingTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		set[name] = struct{}{}
	}
	return set, nil
}

// applyFile applies one migration file and its tracking row atomically.
func (r *Runner) applyFile(ctx context.Context, filename string) error {
	raw, err := os.ReadFile(filepath.Join(r.dir, filename))
	if err != nil {
		return err
	}
	mf, err := ParseFile(raw)
	if err != nil {
		return err
	}

	return r.db.RunInTransactionContext(ctx, func(txCtx context.Context, _ database.Tx) error {
		if err := r.applyMigration(txCtx, mf); err != nil {
			return err
		}
		return r.markApplied(txCtx, filename)
	})
}

// applyMigration upserts every collection snapshot and processes deletions.
// Collections match existing ones by name (the stable cross-environment key);
// views are applied after tables so view queries can reference them.
func (r *Runner) applyMigration(ctx context.Context, mf *MigrationFile) error {
	colls := make([]*collection.Collection, len(mf.Collections))
	copy(colls, mf.Collections)
	sort.SliceStable(colls, func(i, j int) bool {
		return colls[i].Type != collection.TypeView && colls[j].Type == collection.TypeView
	})

	for _, snapshot := range colls {
		// Work on a copy so apply never mutates the parsed file in ways that
		// would surprise repeated use.
		coll := *snapshot

		existing, err := r.svc.GetCollectionByName(ctx, coll.Name)
		switch {
		case err == nil:
			if existing.System {
				return fmt.Errorf("refusing to modify system collection %q", coll.Name)
			}
			coll.ID = existing.ID
			coll.CreatedAt = existing.CreatedAt
			if err := r.svc.UpdateCollection(ctx, &coll); err != nil {
				return fmt.Errorf("update collection %q: %w", coll.Name, err)
			}
		case errors.Is(err, pgx.ErrNoRows):
			if err := r.svc.CreateCollection(ctx, &coll); err != nil {
				return fmt.Errorf("create collection %q: %w", coll.Name, err)
			}
		default:
			return fmt.Errorf("lookup collection %q: %w", coll.Name, err)
		}
	}

	for _, name := range mf.Deleted {
		existing, err := r.svc.GetCollectionByName(ctx, name)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // already gone — deletions are idempotent
		}
		if err != nil {
			return fmt.Errorf("lookup collection %q: %w", name, err)
		}
		if existing.System {
			return fmt.Errorf("refusing to delete system collection %q", name)
		}
		if err := r.svc.DeleteCollection(ctx, existing.ID); err != nil {
			return fmt.Errorf("delete collection %q: %w", name, err)
		}
	}

	return nil
}

// markApplied records a filename in the tracking table. It joins any
// transaction carried by ctx.
func (r *Runner) markApplied(ctx context.Context, filename string) error {
	return r.db.Exec(ctx,
		`INSERT INTO `+TrackingTable+` (filename) VALUES ($1) ON CONFLICT (filename) DO NOTHING`,
		filename)
}

// writeApplied writes a migration file to disk and immediately records it as
// applied: automigrate and snapshot files describe state the live database
// already has, so replaying them locally would be a no-op — but the tracking
// row keeps history clean and skips the redundant work on next boot.
func (r *Runner) writeApplied(ctx context.Context, mf *MigrationFile, slug string) (string, error) {
	filename, err := WriteFile(r.dir, slug, mf)
	if err != nil {
		return "", err
	}
	if err := r.EnsureTable(ctx); err != nil {
		return "", err
	}
	if err := r.markApplied(ctx, filename); err != nil {
		return "", err
	}
	return filename, nil
}

// WriteFile serializes a migration file into dir as
// {unix_timestamp}_{slug}.json, bumping the timestamp on collision so two
// changes in the same second still get distinct, ordered filenames.
// Returns the filename (not the full path).
func WriteFile(dir, slug string, mf *MigrationFile) (string, error) {
	if dir == "" {
		return "", errors.New("migrations directory is not configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create migrations dir %q: %w", dir, err)
	}
	if slug == "" {
		slug = "migration"
	}

	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')

	ts := time.Now().Unix()
	for {
		filename := fmt.Sprintf("%d_%s.json", ts, slug)
		path := filepath.Join(dir, filename)
		if _, err := os.Stat(path); err == nil {
			ts++
			continue
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return "", err
		}
		return filename, nil
	}
}

// ParseFile decodes and validates a migration file. Validation is strict so
// hand-edited files fail fast at apply time instead of half-applying:
// unsupported format versions, nameless collections, and unknown field types
// are all rejected.
func ParseFile(raw []byte) (*MigrationFile, error) {
	mf := &MigrationFile{}
	if err := json.Unmarshal(raw, mf); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if mf.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("unsupported formatVersion %d (this build supports %d)", mf.FormatVersion, FormatVersion)
	}
	if len(mf.Collections) == 0 && len(mf.Deleted) == 0 {
		return nil, errors.New("migration contains no collections and no deletions")
	}
	for i, coll := range mf.Collections {
		if coll == nil || coll.Name == "" {
			return nil, fmt.Errorf("collections[%d]: missing collection name", i)
		}
		for _, field := range coll.Schema {
			if _, ok := knownFieldTypes[field.Type]; !ok {
				return nil, fmt.Errorf("collection %q: unknown field type %q (field %q)", coll.Name, field.Type, field.Name)
			}
		}
	}
	for i, name := range mf.Deleted {
		if name == "" {
			return nil, fmt.Errorf("deleted[%d]: empty collection name", i)
		}
	}
	return mf, nil
}

var knownFieldTypes = map[collection.FieldType]struct{}{
	collection.FieldText:     {},
	collection.FieldNumber:   {},
	collection.FieldBool:     {},
	collection.FieldEmail:    {},
	collection.FieldURL:      {},
	collection.FieldDate:     {},
	collection.FieldSelect:   {},
	collection.FieldJSON:     {},
	collection.FieldFile:     {},
	collection.FieldRelation: {},
	collection.FieldPassword: {},
	collection.FieldEditor:   {},
	collection.FieldGeoPoint: {},
	collection.FieldAutoDate: {},
	collection.FieldVector:   {},
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9_]+`)

// Slugify lowercases a name and reduces it to [a-z0-9_] for use in filenames.
func Slugify(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = slugUnsafe.ReplaceAllString(slug, "_")
	slug = strings.Trim(slug, "_")
	if slug == "" {
		slug = "migration"
	}
	return slug
}
