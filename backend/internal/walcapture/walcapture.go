// Package walcapture sources realtime record events from PostgreSQL logical
// replication (the WAL) instead of the API write handlers. This closes the
// gap where rows changed via the SQL console, psql, the generated RLS role,
// or any other direct database connection were invisible to realtime
// subscribers — and it does so in-process, keeping "PostgreSQL is the only
// infrastructure" honest (no separate change-capture service).
//
// Lifecycle:
//
//	capture := walcapture.New(db, hub, collections, slot, publication)
//	go capture.Run(ctx)   // preflight, publication sync, stream + reconnect
//	...
//	capture.Stop()        // final standby update, close replication conn
//
// Health-aware delivery switch: while the WAL stream is healthy the hub's
// direct BroadcastRecord path is suppressed (the WAL stream is the single
// source of truth, preventing double events for API writes). The moment the
// stream degrades — connection loss, replication error — direct broadcasts
// are re-enabled so realtime keeps working with API-emitted coverage while
// the capture reconnects with backoff.
//
// Multi-node note: every node must use a UNIQUE replication slot
// (realtime_wal_slot). An abandoned slot retains WAL on the server until
// dropped manually: SELECT pg_drop_replication_slot('<slot>');
package walcapture

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gresbase/gresbase/internal/collection"
	"github.com/gresbase/gresbase/internal/database"
	"github.com/gresbase/gresbase/internal/realtime"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/rs/zerolog/log"
)

const (
	// standbyUpdateInterval is how often confirmed_flush_lsn is advanced so
	// the server can recycle WAL behind us.
	standbyUpdateInterval = 10 * time.Second
	// publicationSyncInterval is the reconciliation fallback for publication
	// membership. Collection create/update/delete hooks trigger an immediate
	// sync; this ticker catches anything the hooks missed (e.g. direct DDL).
	publicationSyncInterval = 30 * time.Second
	// schemaCacheTTL bounds staleness of the collection schemas used for
	// tuple decoding (mirrors the realtime rule checker's cache TTL).
	schemaCacheTTL = 5 * time.Second
	// maxReconnectBackoff caps the reconnect delay after stream errors.
	maxReconnectBackoff = 30 * time.Second
)

// slotNamePattern is PostgreSQL's replication slot name rule: lowercase
// letters, digits, and underscores only.
var slotNamePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// Service captures committed row changes from the WAL via a logical
// replication slot and re-emits them as realtime record events through the
// hub's rule-enforced delivery path.
type Service struct {
	db          *database.DB
	hub         *realtime.Hub
	collections *collection.Service
	slot        string
	publication string

	stopCh  chan struct{}
	done    chan struct{}
	stopped atomic.Bool

	healthy        atomic.Bool
	eventsDecoded  atomic.Int64
	streamRestarts atomic.Int64

	mu          sync.Mutex
	schemaCache map[string]cachedSchema
}

type cachedSchema struct {
	fields    map[string]collection.FieldType // nil when the collection does not exist
	fetchedAt time.Time
}

// New creates a WAL change-capture service. Call Run to start it.
func New(db *database.DB, hub *realtime.Hub, collections *collection.Service, slot, publication string) *Service {
	return &Service{
		db:          db,
		hub:         hub,
		collections: collections,
		slot:        strings.ToLower(strings.TrimSpace(slot)),
		publication: strings.ToLower(strings.TrimSpace(publication)),
		stopCh:      make(chan struct{}),
		done:        make(chan struct{}),
		schemaCache: make(map[string]cachedSchema),
	}
}

// Healthy reports whether the replication stream is currently established.
// While true, the hub's direct record broadcasts are suppressed.
func (s *Service) Healthy() bool { return s.healthy.Load() }

// Stats returns counters for metrics: events decoded, stream restarts, and
// the current health state.
func (s *Service) Stats() (decoded, restarts int64, healthy bool) {
	return s.eventsDecoded.Load(), s.streamRestarts.Load(), s.healthy.Load()
}

// Run performs preflight checks and then streams WAL changes until Stop is
// called or ctx is cancelled. Preflight failures (wal_level != logical,
// missing replication permission) log a clear error and return without
// breaking the server: the API-emitted event path stays active.
func (s *Service) Run(ctx context.Context) {
	defer close(s.done)
	defer s.setHealthy(false)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-s.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	if !slotNamePattern.MatchString(s.slot) || database.ValidateIdentifier(s.publication, "publication") != nil {
		log.Error().Str("slot", s.slot).Str("publication", s.publication).
			Msg("WAL capture disabled: realtime_wal_slot must match [a-z0-9_]+ and realtime_wal_publication must be a valid identifier")
		return
	}

	if !s.preflight(ctx) {
		return // reason already logged; API-emitted events remain active
	}

	if err := s.SyncPublication(ctx); err != nil {
		log.Error().Err(err).Str("publication", s.publication).
			Msg("WAL capture disabled: failed to create/sync publication")
		return
	}
	if err := s.ensureSlot(ctx); err != nil {
		log.Error().Err(err).Str("slot", s.slot).
			Msg("WAL capture disabled: failed to create replication slot")
		return
	}

	// Reconciliation fallback for publication membership (collection hooks
	// trigger immediate syncs; this catches direct DDL and missed hooks).
	go s.publicationSyncLoop(ctx)

	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := s.streamOnce(ctx)
		s.setHealthy(false)
		if ctx.Err() != nil {
			return
		}
		s.streamRestarts.Add(1)
		if time.Since(started) > time.Minute {
			backoff = time.Second // stream was stable; reset backoff
		}
		log.Warn().Err(err).Dur("retry_in", backoff).
			Msg("WAL replication stream ended; API-emitted realtime events resume until it reconnects")
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxReconnectBackoff {
			backoff = maxReconnectBackoff
		}
	}
}

// Stop terminates the stream and waits for the replication connection to
// close (a final standby status update is sent on the way out).
func (s *Service) Stop() {
	if s.stopped.Swap(true) {
		return
	}
	close(s.stopCh)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		log.Warn().Msg("WAL capture did not stop within 5s")
	}
}

// setHealthy flips the stream health state and the hub's direct-broadcast
// suppression together: suppress only while the WAL stream actually works.
func (s *Service) setHealthy(healthy bool) {
	s.healthy.Store(healthy)
	if s.hub != nil {
		s.hub.SetDirectRecordBroadcastsSuppressed(healthy)
	}
}

// preflight verifies wal_level and replication permission, logging actionable
// errors when the server cannot support logical decoding.
func (s *Service) preflight(ctx context.Context) bool {
	var walLevel string
	if err := s.db.Pool.QueryRow(ctx, "SHOW wal_level").Scan(&walLevel); err != nil {
		log.Error().Err(err).Msg("WAL capture disabled: could not read wal_level")
		return false
	}
	if walLevel != "logical" {
		log.Error().Str("wal_level", walLevel).
			Msg("WAL capture disabled: wal_level must be 'logical'. Fix: run `ALTER SYSTEM SET wal_level = logical;` as a superuser and restart PostgreSQL. " +
				"(Embedded PostgreSQL is configured automatically when realtime_wal_enabled is set; managed providers usually expose this as a setting.) " +
				"Realtime keeps working with API-emitted events only — changes made via SQL will not reach subscribers.")
		return false
	}

	var canReplicate bool
	err := s.db.Pool.QueryRow(ctx,
		"SELECT rolsuper OR rolreplication FROM pg_roles WHERE rolname = current_user").Scan(&canReplicate)
	if err != nil {
		log.Error().Err(err).Msg("WAL capture disabled: could not check replication permission")
		return false
	}
	if !canReplicate {
		log.Error().
			Msg("WAL capture disabled: the database role lacks REPLICATION. Fix: `ALTER ROLE <user> REPLICATION;`. " +
				"Realtime keeps working with API-emitted events only.")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Publication management
// ---------------------------------------------------------------------------

// SyncPublication creates the publication if absent and reconciles its
// membership with the current set of collection record tables (base and auth
// collections; views and system tables are excluded). Safe to call
// concurrently and cheap enough to call from collection lifecycle hooks.
func (s *Service) SyncPublication(ctx context.Context) error {
	var exists bool
	if err := s.db.Pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = $1)", s.publication).Scan(&exists); err != nil {
		return fmt.Errorf("check publication: %w", err)
	}
	if !exists {
		// Empty publication first; membership is reconciled below. TRUNCATE is
		// intentionally not published — the realtime protocol has no event for it.
		sql := fmt.Sprintf("CREATE PUBLICATION %s WITH (publish = 'insert, update, delete')",
			database.QuoteIdent(s.publication))
		if _, err := s.db.Pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("create publication: %w", err)
		}
		log.Info().Str("publication", s.publication).Msg("Created realtime WAL publication")
	}

	desired, err := s.desiredPublicationTables(ctx)
	if err != nil {
		return err
	}
	current, err := s.currentPublicationTables(ctx)
	if err != nil {
		return err
	}

	for _, stmt := range publicationSyncStatements(s.publication, current, desired) {
		if _, err := s.db.Pool.Exec(ctx, stmt); err != nil {
			// Membership changes can race collection DDL; log and let the
			// periodic reconciliation catch up rather than failing the sync.
			log.Warn().Err(err).Str("stmt", stmt).Msg("WAL publication membership change failed")
		}
	}
	return nil
}

// desiredPublicationTables lists collection record tables that should be in
// the publication: non-view collections whose backing table exists.
func (s *Service) desiredPublicationTables(ctx context.Context) ([]string, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT c.name FROM _collections c
		WHERE c.type <> 'view'
		  AND to_regclass(quote_ident(c.name)) IS NOT NULL
		ORDER BY c.name`)
	if err != nil {
		return nil, fmt.Errorf("list collection tables: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		// Defense in depth: never publish internal tables, and skip names the
		// identifier rules would reject.
		if strings.HasPrefix(name, "_") || !database.IsSafeIdentifier(name) {
			continue
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *Service) currentPublicationTables(ctx context.Context) ([]string, error) {
	rows, err := s.db.Pool.Query(ctx,
		"SELECT tablename FROM pg_publication_tables WHERE pubname = $1 ORDER BY tablename", s.publication)
	if err != nil {
		return nil, fmt.Errorf("list publication tables: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// publicationSyncStatements computes the ALTER PUBLICATION statements that
// reconcile current membership to desired membership. Tables are added and
// dropped individually so an empty desired set works (SET TABLE cannot
// express "no tables").
func publicationSyncStatements(publication string, current, desired []string) []string {
	currentSet := make(map[string]bool, len(current))
	for _, t := range current {
		currentSet[t] = true
	}
	desiredSet := make(map[string]bool, len(desired))
	for _, t := range desired {
		desiredSet[t] = true
	}

	var stmts []string
	for _, t := range desired {
		if !currentSet[t] {
			stmts = append(stmts, fmt.Sprintf("ALTER PUBLICATION %s ADD TABLE %s",
				database.QuoteIdent(publication), database.QuoteIdent(t)))
		}
	}
	for _, t := range current {
		if !desiredSet[t] {
			stmts = append(stmts, fmt.Sprintf("ALTER PUBLICATION %s DROP TABLE %s",
				database.QuoteIdent(publication), database.QuoteIdent(t)))
		}
	}
	return stmts
}

func (s *Service) publicationSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(publicationSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SyncPublication(ctx); err != nil {
				log.Warn().Err(err).Msg("WAL publication reconciliation failed")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Replication slot + stream
// ---------------------------------------------------------------------------

// ensureSlot creates the (permanent, pgoutput) replication slot if absent.
func (s *Service) ensureSlot(ctx context.Context) error {
	var exists bool
	if err := s.db.Pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)", s.slot).Scan(&exists); err != nil {
		return fmt.Errorf("check slot: %w", err)
	}
	if exists {
		return nil
	}

	conn, err := s.connectReplication(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	_, err = pglogrepl.CreateReplicationSlot(ctx, conn, s.slot, "pgoutput",
		pglogrepl.CreateReplicationSlotOptions{})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42710" { // duplicate_object: raced another starter
			return nil
		}
		return fmt.Errorf("create replication slot: %w", err)
	}
	log.Info().Str("slot", s.slot).
		Msg("Created logical replication slot (note: the slot retains WAL while Gresbase is down; drop it manually if you stop using WAL capture)")
	return nil
}

// connectReplication opens a dedicated connection in logical replication mode.
func (s *Service) connectReplication(ctx context.Context) (*pgconn.PgConn, error) {
	cfg, err := pgconn.ParseConfig(s.db.ConnString())
	if err != nil {
		return nil, fmt.Errorf("parse connection string: %w", err)
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["replication"] = "database"
	conn, err := pgconn.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("replication connection: %w", err)
	}
	return conn, nil
}

// streamOnce runs one replication session: START_REPLICATION from the slot's
// confirmed position, decode messages, emit record events, and acknowledge
// progress with periodic standby status updates. Returns when the stream
// errors or ctx is cancelled (sending a final standby update either way).
func (s *Service) streamOnce(ctx context.Context) error {
	conn, err := s.connectReplication(ctx)
	if err != nil {
		return err
	}

	pluginArgs := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", s.publication),
	}
	// LSN 0 starts from the slot's confirmed_flush_lsn, so changes that
	// happened while we were down are replayed, not skipped.
	if err := pglogrepl.StartReplication(ctx, conn, s.slot, pglogrepl.LSN(0),
		pglogrepl.StartReplicationOptions{PluginArgs: pluginArgs}); err != nil {
		conn.Close(context.Background())
		return fmt.Errorf("start replication: %w", err)
	}

	log.Info().Str("slot", s.slot).Str("publication", s.publication).
		Msg("WAL change capture streaming (direct API record broadcasts suppressed)")
	s.setHealthy(true)

	var clientXLogPos pglogrepl.LSN
	relations := make(map[uint32]*pglogrepl.RelationMessage)
	nextStandbyDeadline := time.Now().Add(standbyUpdateInterval)

	defer func() {
		// Final acknowledgement + clean close on the way out.
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if clientXLogPos > 0 {
			_ = pglogrepl.SendStandbyStatusUpdate(closeCtx, conn,
				pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos})
		}
		conn.Close(closeCtx)
	}()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(nextStandbyDeadline) {
			if err := pglogrepl.SendStandbyStatusUpdate(ctx, conn,
				pglogrepl.StandbyStatusUpdate{WALWritePosition: clientXLogPos}); err != nil {
				return fmt.Errorf("standby status update: %w", err)
			}
			nextStandbyDeadline = time.Now().Add(standbyUpdateInterval)
		}

		recvCtx, cancel := context.WithDeadline(ctx, nextStandbyDeadline)
		rawMsg, err := conn.ReceiveMessage(recvCtx)
		cancel()
		if err != nil {
			if pgconn.Timeout(err) && ctx.Err() == nil {
				continue // deadline only paces standby updates
			}
			return fmt.Errorf("receive: %w", err)
		}

		switch msg := rawMsg.(type) {
		case *pgproto3.ErrorResponse:
			return fmt.Errorf("postgres WAL error: %s (%s)", msg.Message, msg.Code)
		case *pgproto3.CopyData:
			switch msg.Data[0] {
			case pglogrepl.PrimaryKeepaliveMessageByteID:
				pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
				if err != nil {
					return fmt.Errorf("parse keepalive: %w", err)
				}
				if pkm.ServerWALEnd > clientXLogPos {
					clientXLogPos = pkm.ServerWALEnd
				}
				if pkm.ReplyRequested {
					nextStandbyDeadline = time.Time{}
				}
			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
				if err != nil {
					return fmt.Errorf("parse xlog data: %w", err)
				}
				s.handleWALMessage(ctx, xld.WALData, relations)
				if pos := xld.WALStart + pglogrepl.LSN(len(xld.WALData)); pos > clientXLogPos {
					clientXLogPos = pos
				}
			}
		}
	}
}

// handleWALMessage decodes one pgoutput message and emits record events for
// Insert/Update/Delete. Decode failures are logged and skipped — a malformed
// event must not kill the stream.
func (s *Service) handleWALMessage(ctx context.Context, walData []byte, relations map[uint32]*pglogrepl.RelationMessage) {
	logicalMsg, err := pglogrepl.Parse(walData)
	if err != nil {
		log.Warn().Err(err).Msg("WAL capture: failed to parse logical replication message")
		return
	}

	switch msg := logicalMsg.(type) {
	case *pglogrepl.RelationMessage:
		relations[msg.RelationID] = msg

	case *pglogrepl.InsertMessage:
		rel := relations[msg.RelationID]
		s.emit(ctx, "create", rel, msg.Tuple)

	case *pglogrepl.UpdateMessage:
		rel := relations[msg.RelationID]
		s.emit(ctx, "update", rel, msg.NewTuple)

	case *pglogrepl.DeleteMessage:
		rel := relations[msg.RelationID]
		s.emitDelete(ctx, rel, msg.OldTuple)

		// Begin/Commit/Origin/Type/Truncate messages carry no record payloads.
	}
}

// emit decodes a tuple into the same JSON shape the API path broadcasts and
// fans it out through the hub's rule-enforced delivery path.
func (s *Service) emit(ctx context.Context, action string, rel *pglogrepl.RelationMessage, tuple *pglogrepl.TupleData) {
	if rel == nil || tuple == nil {
		return
	}
	fields, ok := s.schemaFor(ctx, rel.RelationName)
	if !ok {
		return // not a known collection table (e.g. mid-delete race): skip
	}
	record := decodeTuple(rel, tuple, fields)
	if record == nil {
		return
	}
	id, _ := record["id"].(string)
	if id == "" {
		log.Warn().Str("table", rel.RelationName).Msg("WAL capture: record without id; event skipped")
		return
	}
	s.eventsDecoded.Add(1)
	s.hub.BroadcastRecordFromWAL(action, rel.RelationName, id, record)
}

// emitDelete emits an id-only record, matching the API delete event shape.
// With the default replica identity the old tuple carries only the primary key.
func (s *Service) emitDelete(ctx context.Context, rel *pglogrepl.RelationMessage, oldTuple *pglogrepl.TupleData) {
	if rel == nil || oldTuple == nil {
		return
	}
	if _, ok := s.schemaFor(ctx, rel.RelationName); !ok {
		return
	}
	id := tupleColumnText(rel, oldTuple, "id")
	if id == "" {
		log.Warn().Str("table", rel.RelationName).Msg("WAL capture: delete without id (replica identity?); event skipped")
		return
	}
	s.eventsDecoded.Add(1)
	s.hub.BroadcastRecordFromWAL("delete", rel.RelationName, id, map[string]any{"id": id})
}

// schemaFor returns the field-type map for a collection table, cached with a
// short TTL. ok=false means the table is not a (known) collection — events
// for it are skipped, which fails closed on shape correctness.
func (s *Service) schemaFor(ctx context.Context, tableName string) (map[string]collection.FieldType, bool) {
	now := time.Now()
	s.mu.Lock()
	if entry, ok := s.schemaCache[tableName]; ok && now.Sub(entry.fetchedAt) < schemaCacheTTL {
		s.mu.Unlock()
		return entry.fields, entry.fields != nil
	}
	s.mu.Unlock()

	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	coll, err := s.collections.GetCollectionByName(lookupCtx, tableName)

	var fields map[string]collection.FieldType
	if err == nil && coll != nil && coll.Type != collection.TypeView {
		fields = make(map[string]collection.FieldType, len(coll.Schema))
		for _, f := range coll.Schema {
			fields[f.Name] = f.Type
		}
	}

	s.mu.Lock()
	s.schemaCache[tableName] = cachedSchema{fields: fields, fetchedAt: now}
	s.mu.Unlock()
	return fields, fields != nil
}
