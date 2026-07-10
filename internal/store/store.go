
// Package store provides local SQLite persistence for todoist-aum.
// Uses modernc.org/sqlite (pure Go, no CGO) for zero-dependency cross-compilation.
// FTS5 full-text search indexes are created for searchable content.
package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validIdentifierRE pins ListField's `field` argument to a safe SQL
// identifier shape before any Sprintf interpolation. Matches what
// pragma_table_info implicitly enforces on the primary path, so the
// fallback path inherits the same defense without depending on whether
// the parent's typed domain table exists at the moment of the lookup.
var validIdentifierRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// IsUUID returns true if the input looks like a UUID.
func IsUUID(s string) bool {
	return uuidPattern.MatchString(s)
}

// StoreSchemaVersion is the on-disk schema version this binary understands.
// It is stamped into SQLite's PRAGMA user_version on fresh databases and
// checked on every open.
//
// v3: the "collaborators" domain table moved from an id-only primary key to a
// composite (id, projects_id) key. A collaborator on N shared projects is
// returned once per project by /projects/{id}/collaborators; under the old
// key every row after the first CONFLICTed on id and overwrote projects_id,
// so each user collapsed to a single project. See migrateCollaboratorsCompositeKey.
const StoreSchemaVersion = 3

const resourcesFTSCreateSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS resources_fts USING fts5(
	id, resource_type, content, tokenize='porter unicode61'
)`

type Store struct {
	db *sql.DB
	// writeMu serializes all DB writes. Read paths bypass the lock and run
	// concurrently against WAL. Resource-level concurrency in sync.go.tmpl
	// is 1 (one goroutine per resource via len(resources)-sized work channel)
	// — read-then-write sequences (e.g., GetSyncCursor → SaveSyncState) are
	// race-free by construction within a resource.
	writeMu sync.Mutex
	path    string
}

// Open opens or creates the SQLite store at dbPath using the background
// context. Prefer OpenWithContext from a Cobra command so SIGINT during
// a slow migration interrupts the open instead of stranding the caller.
func Open(dbPath string) (*Store, error) {
	return OpenWithContext(context.Background(), dbPath)
}

// OpenReadOnly opens an existing SQLite store at dbPath in read-only mode.
// mode=ro rejects direct and CTE-wrapped writes (INSERT, UPDATE, DELETE,
// REPLACE, "WITH x AS (...) INSERT ...") at the driver level. Skips
// MkdirAll and migrate; the file is expected to exist.
//
// The file: URI prefix is load-bearing: modernc.org/sqlite only honors
// SQLite's URI query parameters (mode, cache, etc.) when the DSN starts
// with "file:". Without the prefix, "?mode=ro" is silently dropped and
// the connection opens read-write. Pragmas use the driver's _pragma=
// name(value) syntax — modernc.org/sqlite does NOT recognize the
// mattn/go-sqlite3 _journal_mode=WAL / _busy_timeout=5000 form and drops
// those keys silently, so the busy_timeout below is what keeps a read
// concurrent with a writer from failing immediately with SQLITE_BUSY.
func OpenReadOnly(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(268435456)")
	if err != nil {
		return nil, fmt.Errorf("opening database (read-only): %w", err)
	}
	db.SetMaxOpenConns(2)
	return &Store{db: db, path: dbPath}, nil
}

// OpenWithContext opens or creates the SQLite store at dbPath. The
// context is honored by the migration path: cancellation interrupts the
// retry-on-SQLITE_BUSY loop and propagates ctx.Err() back to the caller
// instead of waiting out the full migrationLockTimeout.
func OpenWithContext(ctx context.Context, dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(268435456)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// WAL mode + 2 connections allows one read cursor open while a second
	// query executes (e.g., analytics commands calling helpers during row
	// iteration). Writes are still serialized by SQLite's WAL lock.
	db.SetMaxOpenConns(2)

	s := &Store{db: db, path: dbPath}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Path returns the on-disk path of the backing SQLite file.
func (s *Store) Path() string {
	return s.path
}

// DB exposes the underlying *sql.DB for callers that need to run ad-hoc
// queries (e.g., doctor's cache inspection, share snapshot import).
// Callers must not call Close on the returned handle.
func (s *Store) DB() *sql.DB {
	return s.db
}

// SchemaVersion reads PRAGMA user_version, which is stamped by migrate().
// A zero value means the database predates the schema-version gate — not
// a bug, but the caller may want to warn.
func (s *Store) SchemaVersion() (int, error) {
	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return v, nil
}

// ensureColumn adds a column to an existing table if it isn't already
// present. It is the upgrade-path safety valve for schema additions:
// CREATE TABLE IF NOT EXISTS is a no-op when the table already exists, so
// columns added by newer binaries (e.g. parent_id from the dependent-
// resources work) never land on databases created by older binaries —
// which then trip "no such column" when a follow-on CREATE INDEX runs.
//
// Skips silently if the table doesn't yet exist (fresh install — the
// CREATE TABLE migration will create it with the column already declared)
// or if the column already exists. Runs on the pinned migration
// connection so it sees the writes performed by the in-flight BEGIN
// IMMEDIATE transaction; using s.db here would route through the pool
// and BUSY against the holding writer under concurrent migrators.
func (s *Store) ensureColumn(ctx context.Context, conn *sql.Conn, table, column, decl string) error {
	var name string
	err := conn.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
	).Scan(&name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking table %s: %w", table, err)
	}

	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return fmt.Errorf("table_info %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var n, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &n, &typ, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info %s: %w", table, err)
		}
		if n == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating table_info %s: %w", table, err)
	}

	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "%s" %s`, table, column, decl)); err != nil {
		// A concurrent Open() may have added the column between our
		// PRAGMA check and this ALTER. SQLite returns SQLITE_ERROR with
		// "duplicate column name", which busy_timeout does not retry.
		// The DB is now in the desired state regardless of who won.
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// backfillColumns adds columns that newer binaries declare but that
// pre-existing databases (created before those columns were added) lack.
// Must run before the migrations slice so that subsequent CREATE INDEX
// statements referencing the column can succeed against the upgraded
// table. Idempotent: safe to call on fresh DBs (table-not-found short-
// circuit) and on already-current DBs (column-exists short-circuit).
//
// Table names are emitted bare (no safeName) — ensureColumn double-quotes
// them at SQL emit time and uses parameter binding for the sqlite_master
// lookup, so the values flow as Go string literals first and SQL
// identifiers second. Wrapping with safeName here would embed literal
// double-quote characters into the Go string and break compilation for
// any spec whose dependent-resource snake_cased name is a SQL reserved
// word.
func (s *Store) backfillColumns(ctx context.Context, conn *sql.Conn) error {
	for _, c := range []struct{ table, column, decl string }{
		{table: "comments", column: "content", decl: "TEXT"},
		{table: "comments", column: "file_attachment", decl: "TEXT"},
		{table: "comments", column: "is_deleted", decl: "INTEGER"},
		{table: "comments", column: "posted_at", decl: "DATETIME"},
		{table: "comments", column: "posted_uid", decl: "TEXT"},
		{table: "comments", column: "reactions", decl: "TEXT"},
		{table: "comments", column: "uids_to_notify", decl: "TEXT"},
		{table: "folders", column: "child_order", decl: "INTEGER"},
		{table: "folders", column: "default_order", decl: "INTEGER"},
		{table: "folders", column: "is_deleted", decl: "INTEGER"},
		{table: "folders", column: "name", decl: "TEXT"},
		{table: "folders", column: "workspace_id", decl: "TEXT"},
		{table: "labels", column: "color", decl: "TEXT"},
		{table: "labels", column: "is_favorite", decl: "INTEGER"},
		{table: "labels", column: "name", decl: "TEXT"},
		{table: "labels", column: "order", decl: "TEXT"},
		{table: "location_reminders", column: "is_deleted", decl: "INTEGER"},
		{table: "location_reminders", column: "item_id", decl: "TEXT"},
		{table: "location_reminders", column: "loc_lat", decl: "TEXT"},
		{table: "location_reminders", column: "loc_long", decl: "TEXT"},
		{table: "location_reminders", column: "loc_trigger", decl: "TEXT"},
		{table: "location_reminders", column: "name", decl: "TEXT"},
		{table: "location_reminders", column: "notify_uid", decl: "TEXT"},
		{table: "location_reminders", column: "project_id", decl: "TEXT"},
		{table: "location_reminders", column: "radius", decl: "INTEGER"},
		{table: "location_reminders", column: "type", decl: "TEXT"},
		{table: "payments", column: "activation_method", decl: "TEXT"},
		{table: "payments", column: "billing_portal_switch_to_annual_url", decl: "TEXT"},
		{table: "payments", column: "billing_portal_url", decl: "TEXT"},
		{table: "payments", column: "expiration_date", decl: "TEXT"},
		{table: "payments", column: "has_billing_portal", decl: "INTEGER"},
		{table: "payments", column: "has_billing_portal_switch_to_annual", decl: "INTEGER"},
		{table: "payments", column: "has_switch_legacy_to_current", decl: "INTEGER"},
		{table: "payments", column: "invoice_credit_balance", decl: "TEXT"},
		{table: "payments", column: "plan", decl: "TEXT"},
		{table: "payments", column: "plan_price", decl: "TEXT"},
		{table: "payments", column: "status", decl: "TEXT"},
		{table: "projects_archive", column: "projects_id", decl: "TEXT"},
		{table: "collaborators", column: "projects_id", decl: "TEXT"},
		{table: "collaborators", column: "parent_id", decl: "TEXT"},
		{table: "join", column: "projects_id", decl: "TEXT"},
		{table: "projects_unarchive", column: "projects_id", decl: "TEXT"},
		{table: "reminders", column: "due", decl: "TEXT"},
		{table: "reminders", column: "is_deleted", decl: "INTEGER"},
		{table: "reminders", column: "is_urgent", decl: "INTEGER"},
		{table: "reminders", column: "item_id", decl: "TEXT"},
		{table: "reminders", column: "minute_offset", decl: "TEXT"},
		{table: "reminders", column: "notify_uid", decl: "TEXT"},
		{table: "reminders", column: "type", decl: "TEXT"},
		{table: "sections", column: "added_at", decl: "DATETIME"},
		{table: "sections", column: "archived_at", decl: "DATETIME"},
		{table: "sections", column: "is_archived", decl: "INTEGER"},
		{table: "sections", column: "is_collapsed", decl: "INTEGER"},
		{table: "sections", column: "is_deleted", decl: "INTEGER"},
		{table: "sections", column: "name", decl: "TEXT"},
		{table: "sections", column: "project_id", decl: "TEXT"},
		{table: "sections", column: "section_order", decl: "INTEGER"},
		{table: "sections", column: "updated_at", decl: "DATETIME"},
		{table: "sections", column: "user_id", decl: "TEXT"},
		{table: "sections_archive", column: "sections_id", decl: "TEXT"},
		{table: "sections_unarchive", column: "sections_id", decl: "TEXT"},
		{table: "tasks", column: "next_cursor", decl: "TEXT"},
		{table: "tasks", column: "completed_count", decl: "INTEGER"},
		{table: "tasks", column: "karma", decl: "REAL"},
		{table: "tasks", column: "karma_last_update", decl: "REAL"},
		{table: "tasks", column: "karma_trend", decl: "TEXT"},
		{table: "tasks", column: "added_at", decl: "DATETIME"},
		{table: "tasks", column: "added_by_uid", decl: "TEXT"},
		{table: "tasks", column: "assigned_by_uid", decl: "TEXT"},
		{table: "tasks", column: "checked", decl: "INTEGER"},
		{table: "tasks", column: "child_order", decl: "INTEGER"},
		{table: "tasks", column: "completed_at", decl: "DATETIME"},
		{table: "tasks", column: "completed_by_uid", decl: "TEXT"},
		{table: "tasks", column: "content", decl: "TEXT"},
		{table: "tasks", column: "day_order", decl: "INTEGER"},
		{table: "tasks", column: "deadline", decl: "TEXT"},
		{table: "tasks", column: "description", decl: "TEXT"},
		{table: "tasks", column: "due", decl: "TEXT"},
		{table: "tasks", column: "duration", decl: "TEXT"},
		{table: "tasks", column: "is_collapsed", decl: "INTEGER"},
		{table: "tasks", column: "is_deleted", decl: "INTEGER"},
		{table: "tasks", column: "note_count", decl: "INTEGER"},
		{table: "tasks", column: "parent_id", decl: "TEXT"},
		{table: "tasks", column: "priority", decl: "INTEGER"},
		{table: "tasks", column: "project_id", decl: "TEXT"},
		{table: "tasks", column: "responsible_uid", decl: "TEXT"},
		{table: "tasks", column: "section_id", decl: "TEXT"},
		{table: "tasks", column: "updated_at", decl: "DATETIME"},
		{table: "tasks", column: "user_id", decl: "TEXT"},
		{table: "close", column: "tasks_id", decl: "TEXT"},
		{table: "move", column: "tasks_id", decl: "TEXT"},
		{table: "reopen", column: "tasks_id", decl: "TEXT"},
		{table: "templates", column: "project_id", decl: "TEXT"},
		{table: "templates", column: "status", decl: "TEXT"},
		{table: "templates", column: "template_type", decl: "TEXT"},
		{table: "uploads", column: "file_name", decl: "TEXT"},
		{table: "uploads", column: "file_size", decl: "INTEGER"},
		{table: "uploads", column: "file_type", decl: "TEXT"},
		{table: "uploads", column: "file_url", decl: "TEXT"},
		{table: "uploads", column: "image", decl: "TEXT"},
		{table: "uploads", column: "image_height", decl: "TEXT"},
		{table: "uploads", column: "image_width", decl: "TEXT"},
		{table: "uploads", column: "resource_type", decl: "TEXT"},
		{table: "uploads", column: "upload_state", decl: "TEXT"},
		{table: "workspaces", column: "inviter_id", decl: "TEXT"},
		{table: "workspaces", column: "is_existing_user", decl: "INTEGER"},
		{table: "workspaces", column: "role", decl: "TEXT"},
		{table: "workspaces", column: "user_email", decl: "TEXT"},
		{table: "workspaces", column: "workspace_id", decl: "TEXT"},
		{table: "workspaces_projects", column: "workspaces_id", decl: "TEXT"},
		{table: "workspaces_projects", column: "parent_id", decl: "TEXT"},
		{table: "users", column: "workspaces_id", decl: "TEXT"},
		{table: "sync_state", column: "last_cursor", decl: "TEXT"},
		{table: "sync_state", column: "last_synced_at", decl: "DATETIME"},
		{table: "sync_state", column: "total_count", decl: "INTEGER DEFAULT 0"},
	} {
		if err := s.ensureColumn(ctx, conn, c.table, c.column, c.decl); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	// Acquiring the migration connection establishes a physical SQLite
	// connection, which runs the DSN _pragma directives — including the
	// journal_mode(WAL) conversion. On a fresh DB opened by several
	// processes at once, that conversion briefly needs an exclusive lock
	// and can return SQLITE_BUSY before any statement-level busy handler
	// applies, so retry the acquisition against the shared deadline.
	deadline := time.Now().Add(migrationLockTimeout)
	var conn *sql.Conn
	if err := retryOnBusy(ctx, deadline, "acquiring migration connection", func() error {
		c, err := s.db.Conn(ctx)
		if err != nil {
			return err
		}
		conn = c
		return nil
	}); err != nil {
		return err
	}
	defer conn.Close()

	// Read user_version before the migration lock so an old binary
	// opening a newer-schema DB rejects immediately. WAL readers don't
	// normally block on writers, but the fresh-DB WAL-init race can BUSY
	// a SELECT — share the lock's deadline so total budget stays bounded.
	var current int
	if err := retryOnBusy(ctx, deadline, "reading schema version", func() error {
		return conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current)
	}); err != nil {
		return err
	}
	if current > StoreSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d; upgrade the CLI binary or open an older database", current, StoreSchemaVersion)
	}

	migrations := []string{
		`CREATE TABLE IF NOT EXISTS resources (
			id TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			data JSON NOT NULL,
			synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (resource_type, id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_resources_type ON resources(resource_type)`,
		`CREATE INDEX IF NOT EXISTS idx_resources_synced ON resources(synced_at)`,
		`CREATE TABLE IF NOT EXISTS sync_state (
			resource_type TEXT PRIMARY KEY,
			last_cursor TEXT,
			last_synced_at DATETIME,
			total_count INTEGER DEFAULT 0
		)`,
		resourcesFTSCreateSQL,
		`CREATE TABLE IF NOT EXISTS "comments" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"content" TEXT,
			"file_attachment" TEXT,
			"is_deleted" INTEGER,
			"posted_at" DATETIME,
			"posted_uid" TEXT,
			"reactions" TEXT,
			"uids_to_notify" TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS "folders" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"child_order" INTEGER,
			"default_order" INTEGER,
			"is_deleted" INTEGER,
			"name" TEXT,
			"workspace_id" TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_folders_workspace_id" ON "folders"("workspace_id")`,
		`CREATE TABLE IF NOT EXISTS "labels" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"color" TEXT,
			"is_favorite" INTEGER,
			"name" TEXT,
			"order" TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS "location_reminders" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"is_deleted" INTEGER,
			"item_id" TEXT,
			"loc_lat" TEXT,
			"loc_long" TEXT,
			"loc_trigger" TEXT,
			"name" TEXT,
			"notify_uid" TEXT,
			"project_id" TEXT,
			"radius" INTEGER,
			"type" TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_location_reminders_item_id" ON "location_reminders"("item_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_location_reminders_project_id" ON "location_reminders"("project_id")`,
		`CREATE TABLE IF NOT EXISTS "payments" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"activation_method" TEXT,
			"billing_portal_switch_to_annual_url" TEXT,
			"billing_portal_url" TEXT,
			"expiration_date" TEXT,
			"has_billing_portal" INTEGER,
			"has_billing_portal_switch_to_annual" INTEGER,
			"has_switch_legacy_to_current" INTEGER,
			"invoice_credit_balance" TEXT,
			"plan" TEXT,
			"plan_price" TEXT,
			"status" TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS "projects_archive" (
			"id" TEXT PRIMARY KEY,
			"projects_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_projects_archive_projects_id" ON "projects_archive"("projects_id")`,
		// Composite (id, projects_id) key: a collaborator appears once per
		// shared project, so the same user id must coexist across projects.
		`CREATE TABLE IF NOT EXISTS "collaborators" (
			"id" TEXT NOT NULL,
			"projects_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"parent_id" TEXT,
			PRIMARY KEY ("id", "projects_id")
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_collaborators_projects_id" ON "collaborators"("projects_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_collaborators_parent_id" ON "collaborators"("parent_id")`,
		`CREATE TABLE IF NOT EXISTS "join" (
			"id" TEXT PRIMARY KEY,
			"projects_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_join_projects_id" ON "join"("projects_id")`,
		`CREATE TABLE IF NOT EXISTS "projects_unarchive" (
			"id" TEXT PRIMARY KEY,
			"projects_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_projects_unarchive_projects_id" ON "projects_unarchive"("projects_id")`,
		`CREATE TABLE IF NOT EXISTS "reminders" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"due" TEXT,
			"is_deleted" INTEGER,
			"is_urgent" INTEGER,
			"item_id" TEXT,
			"minute_offset" TEXT,
			"notify_uid" TEXT,
			"type" TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_reminders_item_id" ON "reminders"("item_id")`,
		`CREATE TABLE IF NOT EXISTS "sections" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"added_at" DATETIME,
			"archived_at" DATETIME,
			"is_archived" INTEGER,
			"is_collapsed" INTEGER,
			"is_deleted" INTEGER,
			"name" TEXT,
			"project_id" TEXT,
			"section_order" INTEGER,
			"updated_at" DATETIME,
			"user_id" TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_sections_project_id" ON "sections"("project_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_sections_user_id" ON "sections"("user_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_sections_updated_at" ON "sections"("updated_at")`,
		`CREATE TABLE IF NOT EXISTS "sections_archive" (
			"id" TEXT PRIMARY KEY,
			"sections_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_sections_archive_sections_id" ON "sections_archive"("sections_id")`,
		`CREATE TABLE IF NOT EXISTS "sections_unarchive" (
			"id" TEXT PRIMARY KEY,
			"sections_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_sections_unarchive_sections_id" ON "sections_unarchive"("sections_id")`,
		`CREATE TABLE IF NOT EXISTS "tasks" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"next_cursor" TEXT,
			"completed_count" INTEGER,
			"karma" REAL,
			"karma_last_update" REAL,
			"karma_trend" TEXT,
			"added_at" DATETIME,
			"added_by_uid" TEXT,
			"assigned_by_uid" TEXT,
			"checked" INTEGER,
			"child_order" INTEGER,
			"completed_at" DATETIME,
			"completed_by_uid" TEXT,
			"content" TEXT,
			"day_order" INTEGER,
			"deadline" TEXT,
			"description" TEXT,
			"due" TEXT,
			"duration" TEXT,
			"is_collapsed" INTEGER,
			"is_deleted" INTEGER,
			"note_count" INTEGER,
			"parent_id" TEXT,
			"priority" INTEGER,
			"project_id" TEXT,
			"responsible_uid" TEXT,
			"section_id" TEXT,
			"updated_at" DATETIME,
			"user_id" TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_tasks_parent_id" ON "tasks"("parent_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_tasks_project_id" ON "tasks"("project_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_tasks_section_id" ON "tasks"("section_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_tasks_user_id" ON "tasks"("user_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_tasks_updated_at" ON "tasks"("updated_at")`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS "tasks_fts" USING fts5(
			"content",
			"description",
			content='tasks',
			content_rowid='rowid'
		)`,
		`CREATE TRIGGER IF NOT EXISTS "tasks_ai" AFTER INSERT ON "tasks" BEGIN
			INSERT INTO "tasks_fts"(rowid, "content", "description")
			VALUES (new.rowid,new."content", new."description");
		END`,
		`CREATE TRIGGER IF NOT EXISTS "tasks_ad" AFTER DELETE ON "tasks" BEGIN
			INSERT INTO "tasks_fts"("tasks_fts", rowid, "content", "description")
			VALUES ('delete', old.rowid,old."content", old."description");
		END`,
		`CREATE TRIGGER IF NOT EXISTS "tasks_au" AFTER UPDATE ON "tasks" BEGIN
			INSERT INTO "tasks_fts"("tasks_fts", rowid, "content", "description")
			VALUES ('delete', old.rowid,old."content", old."description");
			INSERT INTO "tasks_fts"(rowid, "content", "description")
			VALUES (new.rowid,new."content", new."description");
		END`,
		`CREATE TABLE IF NOT EXISTS "close" (
			"id" TEXT PRIMARY KEY,
			"tasks_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_close_tasks_id" ON "close"("tasks_id")`,
		`CREATE TABLE IF NOT EXISTS "move" (
			"id" TEXT PRIMARY KEY,
			"tasks_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_move_tasks_id" ON "move"("tasks_id")`,
		`CREATE TABLE IF NOT EXISTS "reopen" (
			"id" TEXT PRIMARY KEY,
			"tasks_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_reopen_tasks_id" ON "reopen"("tasks_id")`,
		`CREATE TABLE IF NOT EXISTS "templates" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"project_id" TEXT,
			"status" TEXT,
			"template_type" TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_templates_project_id" ON "templates"("project_id")`,
		`CREATE TABLE IF NOT EXISTS "uploads" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"file_name" TEXT,
			"file_size" INTEGER,
			"file_type" TEXT,
			"file_url" TEXT,
			"image" TEXT,
			"image_height" TEXT,
			"image_width" TEXT,
			"resource_type" TEXT,
			"upload_state" TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS "workspaces" (
			"id" TEXT PRIMARY KEY,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"inviter_id" TEXT,
			"is_existing_user" INTEGER,
			"role" TEXT,
			"user_email" TEXT,
			"workspace_id" TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_workspaces_inviter_id" ON "workspaces"("inviter_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_workspaces_workspace_id" ON "workspaces"("workspace_id")`,
		`CREATE TABLE IF NOT EXISTS "workspaces_projects" (
			"id" TEXT PRIMARY KEY,
			"workspaces_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
			"parent_id" TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_workspaces_projects_workspaces_id" ON "workspaces_projects"("workspaces_id")`,
		`CREATE INDEX IF NOT EXISTS "idx_workspaces_projects_parent_id" ON "workspaces_projects"("parent_id")`,
		`CREATE TABLE IF NOT EXISTS "users" (
			"id" TEXT PRIMARY KEY,
			"workspaces_id" TEXT NOT NULL,
			"data" JSON NOT NULL,
			"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS "idx_users_workspaces_id" ON "users"("workspaces_id")`,
	}

	// Run every migration — including the column backfill and the
	// schema-version stamp — inside a single BEGIN IMMEDIATE transaction
	// pinned to one connection. IMMEDIATE acquires SQLite's RESERVED lock
	// at BEGIN time so concurrent migrators serialize on it instead of
	// racing per-statement and tripping SQLITE_BUSY despite busy_timeout.
	// modernc.org/sqlite's busy_timeout does not always cover write-write
	// contention at BEGIN/COMMIT time, so we retry both explicitly on
	// SQLITE_BUSY for up to migrationLockTimeout.
	return withMigrationLock(ctx, conn, deadline, func() error {
		// Re-read user_version inside the lock. This is load-bearing,
		// not paranoid: between the pre-lock read above and our
		// successful BEGIN IMMEDIATE, a newer-binary peer may have
		// committed a higher version stamp. Without this re-read, an
		// older binary (smaller StoreSchemaVersion) would proceed to
		// stamp its own lower version at the end of the closure,
		// silently downgrading user_version on a schema that's already
		// at the newer level. Future maintainers: leave this read in.
		var current int
		if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current); err != nil {
			return fmt.Errorf("reading schema version: %w", err)
		}
		if current > StoreSchemaVersion {
			return fmt.Errorf("database schema version %d is newer than supported version %d; upgrade the CLI binary or open an older database", current, StoreSchemaVersion)
		}

		if current < 2 {
			if err := s.migrateResourcesCompositeKey(ctx, conn); err != nil {
				return fmt.Errorf("migrating resources composite key: %w", err)
			}
		}

		if current < 3 {
			if err := s.migrateCollaboratorsCompositeKey(ctx, conn); err != nil {
				return fmt.Errorf("migrating collaborators composite key: %w", err)
			}
		}

		if err := s.backfillColumns(ctx, conn); err != nil {
			return fmt.Errorf("backfilling columns: %w", err)
		}
		for _, m := range migrations {
			if _, err := conn.ExecContext(ctx, m); err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}
		}
		if err := s.migrateExtras(ctx, conn); err != nil {
			return fmt.Errorf("running extra migrations: %w", err)
		}
		// Stamp the schema version. On a fresh DB this writes the current
		// StoreSchemaVersion; on an already-stamped DB this is a no-op
		// write of the same value.
		// An older DB with user_version = 0 and pre-existing tables gets
		// stamped here after any version-gated rewrites and idempotent
		// CREATE TABLE IF NOT EXISTS statements have completed.
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, StoreSchemaVersion)); err != nil {
			return fmt.Errorf("stamp user_version: %w", err)
		}
		return nil
	})
}

func (s *Store) migrateResourcesCompositeKey(ctx context.Context, conn *sql.Conn) error {
	exists, err := tableExists(ctx, conn, "resources")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	composite, err := resourcesTableHasCompositeKey(ctx, conn)
	if err != nil {
		return err
	}
	if !composite {
		if _, err := conn.ExecContext(ctx, `CREATE TABLE resources_v2 (
			id TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			data JSON NOT NULL,
			synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (resource_type, id)
		)`); err != nil {
			return fmt.Errorf("creating resources_v2: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO resources_v2 (id, resource_type, data, synced_at, updated_at)
			SELECT id, resource_type, data, synced_at, updated_at FROM resources`); err != nil {
			return fmt.Errorf("copying resources rows: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `DROP TABLE resources`); err != nil {
			return fmt.Errorf("dropping old resources table: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `ALTER TABLE resources_v2 RENAME TO resources`); err != nil {
			return fmt.Errorf("renaming resources_v2: %w", err)
		}
	}

	// Always rebuild FTS during the v2 transition. The resources table may
	// already have the composite key, but v1 FTS rowids were scoped by id
	// alone and must be replaced with resource_type + id rowids.
	if _, err := conn.ExecContext(ctx, `DROP TABLE IF EXISTS resources_fts`); err != nil {
		return fmt.Errorf("dropping resources_fts: %w", err)
	}
	if _, err := conn.ExecContext(ctx, resourcesFTSCreateSQL); err != nil {
		return fmt.Errorf("creating resources_fts: %w", err)
	}
	if err := rebuildResourcesFTS(ctx, conn); err != nil {
		return fmt.Errorf("rebuilding resources_fts: %w", err)
	}
	return nil
}

// migrateCollaboratorsCompositeKey rebuilds the "collaborators" table with a
// composite (id, projects_id) primary key. The v1/v2 table keyed on id alone,
// so a user shared on multiple projects kept only the last-synced project row
// (every later INSERT CONFLICTed on id and overwrote projects_id). Existing
// rows are already collapsed to one-per-id; copying them is loss-free, and the
// next sync repopulates the full (user, project) matrix now that the key admits
// it. Unlike resources, this table has no FTS mirror to rebuild.
func (s *Store) migrateCollaboratorsCompositeKey(ctx context.Context, conn *sql.Conn) error {
	exists, err := tableExists(ctx, conn, "collaborators")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	composite, err := collaboratorsTableHasCompositeKey(ctx, conn)
	if err != nil {
		return err
	}
	if composite {
		return nil
	}

	if _, err := conn.ExecContext(ctx, `CREATE TABLE "collaborators_v2" (
		"id" TEXT NOT NULL,
		"projects_id" TEXT NOT NULL,
		"data" JSON NOT NULL,
		"synced_at" DATETIME DEFAULT CURRENT_TIMESTAMP,
		"parent_id" TEXT,
		PRIMARY KEY ("id", "projects_id")
	)`); err != nil {
		return fmt.Errorf("creating collaborators_v2: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO "collaborators_v2" ("id", "projects_id", "data", "synced_at", "parent_id")
		SELECT "id", "projects_id", "data", "synced_at", "parent_id" FROM "collaborators"`); err != nil {
		return fmt.Errorf("copying collaborators rows: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `DROP TABLE "collaborators"`); err != nil {
		return fmt.Errorf("dropping old collaborators table: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `ALTER TABLE "collaborators_v2" RENAME TO "collaborators"`); err != nil {
		return fmt.Errorf("renaming collaborators_v2: %w", err)
	}
	// Recreate the secondary indexes dropped with the old table. The
	// idempotent CREATE INDEX IF NOT EXISTS statements in the migrations
	// slice also cover this, but doing it here keeps the rebuild self-contained.
	if _, err := conn.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS "idx_collaborators_projects_id" ON "collaborators"("projects_id")`); err != nil {
		return fmt.Errorf("recreating idx_collaborators_projects_id: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS "idx_collaborators_parent_id" ON "collaborators"("parent_id")`); err != nil {
		return fmt.Errorf("recreating idx_collaborators_parent_id: %w", err)
	}
	return nil
}

func collaboratorsTableHasCompositeKey(ctx context.Context, conn *sql.Conn) (bool, error) {
	rows, err := conn.QueryContext(ctx, `PRAGMA table_info(collaborators)`)
	if err != nil {
		return false, fmt.Errorf("reading collaborators table info: %w", err)
	}
	defer rows.Close()

	pk := map[string]int{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pkOrder int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pkOrder); err != nil {
			return false, fmt.Errorf("scanning collaborators table info: %w", err)
		}
		pk[name] = pkOrder
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("reading collaborators table info rows: %w", err)
	}
	return pk["id"] == 1 && pk["projects_id"] == 2, nil
}

func tableExists(ctx context.Context, conn *sql.Conn, name string) (bool, error) {
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		return false, fmt.Errorf("checking table %s: %w", name, err)
	}
	return count > 0, nil
}

func resourcesTableHasCompositeKey(ctx context.Context, conn *sql.Conn) (bool, error) {
	rows, err := conn.QueryContext(ctx, `PRAGMA table_info(resources)`)
	if err != nil {
		return false, fmt.Errorf("reading resources table info: %w", err)
	}
	defer rows.Close()

	pk := map[string]int{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pkOrder int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pkOrder); err != nil {
			return false, fmt.Errorf("scanning resources table info: %w", err)
		}
		pk[name] = pkOrder
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("reading resources table info rows: %w", err)
	}
	return pk["resource_type"] == 1 && pk["id"] == 2, nil
}

func rebuildResourcesFTS(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `SELECT id, resource_type, data FROM resources`)
	if err != nil {
		return fmt.Errorf("querying resources: %w", err)
	}

	type resourceRow struct {
		id           string
		resourceType string
		data         string
	}
	var resources []resourceRow
	for rows.Next() {
		var r resourceRow
		if err := rows.Scan(&r.id, &r.resourceType, &r.data); err != nil {
			rows.Close()
			return fmt.Errorf("scanning resource: %w", err)
		}
		resources = append(resources, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reading resource rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("closing resource rows: %w", err)
	}

	for _, r := range resources {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO resources_fts (rowid, id, resource_type, content) VALUES (?, ?, ?, ?)`,
			ftsRowID(r.resourceType, r.id), r.id, r.resourceType, r.data,
		); err != nil {
			return fmt.Errorf("indexing resource %s/%s: %w", r.resourceType, r.id, err)
		}
	}
	return nil
}

const (
	migrationLockTimeout    = 30 * time.Second
	migrationLockBackoffMin = 5 * time.Millisecond
	migrationLockBackoffMax = 100 * time.Millisecond
)

// withMigrationLock runs fn inside a BEGIN IMMEDIATE / COMMIT pair on
// conn, retrying both BEGIN and COMMIT on SQLITE_BUSY against the
// caller-provided deadline. Sharing the deadline with the pre-lock
// version read keeps total Open() latency bounded by a single budget.
// The real upper bound is deadline + one trailing backoff interval
// (≤100ms) + the driver's busy_timeout for the in-flight Exec, since
// the deadline is checked after each failed attempt rather than as a
// hard wall-clock cutoff. fn must use conn (not s.db) so its writes
// participate in the held transaction.
func withMigrationLock(ctx context.Context, conn *sql.Conn, deadline time.Time, fn func() error) error {
	if err := execWithBusyRetry(ctx, conn, "BEGIN IMMEDIATE", "begin migration transaction", deadline); err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// ROLLBACK uses context.Background() so caller-context cancellation
		// can't strand the connection in an open transaction. A failed
		// rollback is rare on local SQLite (broken file handle, fatal
		// driver error) but worth surfacing — silent swallow leaves a
		// pinned connection returned to the pool with state that will
		// confuse later queries.
		if _, rerr := conn.ExecContext(context.Background(), "ROLLBACK"); rerr != nil {
			fmt.Fprintf(os.Stderr, "warning: store migration rollback failed: %v\n", rerr)
		}
	}()

	if err := fn(); err != nil {
		return err
	}

	if err := execWithBusyRetry(ctx, conn, "COMMIT", "commit migration transaction", deadline); err != nil {
		return err
	}
	committed = true
	return nil
}

// execWithBusyRetry runs stmt on conn and retries on SQLITE_BUSY until
// deadline. It covers BEGIN IMMEDIATE and COMMIT contention;
// modernc.org/sqlite's busy_timeout does not reliably cover either when
// multiple connections race for the WAL write lock.
func execWithBusyRetry(ctx context.Context, conn *sql.Conn, stmt, label string, deadline time.Time) error {
	return retryOnBusy(ctx, deadline, label, func() error {
		_, err := conn.ExecContext(ctx, stmt)
		return err
	})
}

// retryOnBusy runs op and retries it on SQLITE_BUSY/LOCKED until
// deadline. The same retry shape covers Exec, Query, and any other
// SQLite call that can race the WAL writer lock — including the
// pre-lock user_version read, where the WAL initialization race on a
// fresh DB can BUSY a SELECT that should otherwise succeed under WAL
// reader/writer concurrency.
func retryOnBusy(ctx context.Context, deadline time.Time, label string, op func() error) error {
	backoff := migrationLockBackoffMin
	for {
		err := op()
		if err == nil {
			return nil
		}
		if !isSQLiteBusy(err) {
			return fmt.Errorf("%s: %w", label, err)
		}
		if time.Now().After(deadline) {
			// The label carries the operation context (e.g. "begin
			// migration transaction", "reading schema version") — we
			// don't hardcode "waiting for write lock" because pre-lock
			// reads also flow through this helper.
			return fmt.Errorf("%s: timed out after %s under SQLite contention: %w", label, migrationLockTimeout, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", label, ctx.Err())
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, migrationLockBackoffMax)
	}
}

// isSQLiteBusy reports whether err is a retryable SQLite lock condition.
// Covers both the file-level WAL writer race (SQLITE_BUSY / "database is
// locked") and the table-level shared-cache contention (SQLITE_LOCKED /
// "database table is locked"). The match is on the error string because
// modernc.org/sqlite does not export an error type the generated code
// can switch on without dragging the driver package into every store
// consumer.
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "SQLITE_LOCKED") ||
		strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked")
}

func (s *Store) upsertGenericResourceTx(tx *sql.Tx, resourceType, id string, data json.RawMessage) error {
	_, err := tx.Exec(
		`INSERT INTO resources (id, resource_type, data, synced_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(resource_type, id) DO UPDATE SET data = excluded.data, synced_at = excluded.synced_at, updated_at = excluded.updated_at`,
		id, resourceType, string(data), time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return err
	}

	ftsRowid := ftsRowID(resourceType, id)
	// Use explicit rowid for FTS5 compatibility with modernc.org/sqlite.
	// Standard DELETE WHERE column=? may not work on FTS5 virtual tables.
	if _, err = tx.Exec(`DELETE FROM resources_fts WHERE rowid = ?`, ftsRowid); err != nil {
		fmt.Fprintf(os.Stderr, "warning: FTS index cleanup failed: %v\n", err)
	}

	if _, err = tx.Exec(
		`INSERT INTO resources_fts (rowid, id, resource_type, content)
		 VALUES (?, ?, ?, ?)`,
		ftsRowid, id, resourceType, string(data),
	); err != nil {
		// FTS insert failure is non-fatal
		fmt.Fprintf(os.Stderr, "warning: FTS index update failed: %v\n", err)
	}

	return nil
}

func (s *Store) Upsert(resourceType, id string, data json.RawMessage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, resourceType, id, data); err != nil {
		return err
	}

	return tx.Commit()
}

// Propagates sql.ErrNoRows on a miss so callers can distinguish absence from
// other scan errors via errors.Is.
func (s *Store) Get(resourceType, id string) (json.RawMessage, error) {
	var data string
	err := s.db.QueryRow(
		`SELECT data FROM resources WHERE resource_type = ? AND id = ?`,
		resourceType, id,
	).Scan(&data)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func (s *Store) List(resourceType string, limit int) ([]json.RawMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(
		`SELECT data FROM resources WHERE resource_type = ? ORDER BY updated_at DESC LIMIT ?`,
		resourceType, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		results = append(results, json.RawMessage(data))
	}
	return results, rows.Err()
}

func (s *Store) Search(query string, limit int) ([]json.RawMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT r.data FROM resources r
		 JOIN resources_fts f ON r.id = f.id AND r.resource_type = f.resource_type
		 WHERE resources_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		results = append(results, json.RawMessage(data))
	}
	return results, rows.Err()
}

func extractObjectID(obj map[string]any) string {
	for _, key := range []string{"id", "Id", "ID", "uuid", "slug", "name"} {
		if v, ok := obj[key]; ok {
			return ResourceIDString(v)
		}
	}
	return ""
}

// ftsRowID derives a deterministic rowid from a string ID for use with FTS5.
// modernc.org/sqlite's FTS5 implementation may not support DELETE WHERE column=?
// on virtual tables, so we use explicit rowids and DELETE WHERE rowid=? instead.
func ftsRowID(scope, id string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(scope))
	_, _ = h.Write([]byte{0}) // separator so ("ab","c") != ("a","bc")
	_, _ = h.Write([]byte(id))
	return int64(h.Sum64() & 0x7FFFFFFFFFFFFFFF) // ensure positive
}

// LookupFieldValue resolves a field value from a JSON object map, trying the
// snake_case key first, then the camelCase rendering, then the PascalCase
// rendering. Exported so the sync command's extractID and the upsert path
// resolve fields the same way — a divergence here produces silent drops on
// heterogeneous payloads. The PascalCase pass handles .NET-shaped responses
// (`Id`, `Name`, `OrderId`) without forcing each spec to declare casing.
func LookupFieldValue(obj map[string]any, snakeKey string) any {
	if v, ok := obj[snakeKey]; ok {
		return sqliteFieldValue(v)
	}
	parts := strings.Split(snakeKey, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	camel := strings.Join(parts, "")
	if v, ok := obj[camel]; ok {
		return sqliteFieldValue(v)
	}
	if parts[0] != "" {
		pascal := strings.ToUpper(parts[0][:1]) + parts[0][1:] + strings.Join(parts[1:], "")
		if v, ok := obj[pascal]; ok {
			return sqliteFieldValue(v)
		}
	}
	return nil
}

func sqliteFieldValue(v any) any {
	switch t := v.(type) {
	case nil, string, bool, int, int64, float64, []byte:
		return v
	case json.Number:
		return strings.TrimSpace(t.String())
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

// lookupFieldValue is kept as an unexported alias for in-package callers so
// the existing UpsertBatch code reads naturally without prefixing every call
// with the package name.
func lookupFieldValue(obj map[string]any, snakeKey string) any {
	return LookupFieldValue(obj, snakeKey)
}

// DecodeJSONObject decodes data into an object while preserving JSON numbers.
// Plain json.Unmarshal turns numbers into float64, and fmt on those values can
// render large integer IDs as scientific notation before they reach resources.id.
func DecodeJSONObject(data json.RawMessage) (map[string]any, error) {
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// ResourceIDString returns the stable text form used for resources.id.
func ResourceIDString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case json.Number:
		return strings.TrimSpace(t.String())
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return ""
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		f := float64(t)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return ""
		}
		return strconv.FormatFloat(f, 'f', -1, 32)
	default:
		// fmt.Sprint on typed nil pointers returns "<nil>"; callers still guard
		// that sentinel so unresolved IDs do not become stored resource keys.
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

// upsertCommentsTx writes the per-resource domain-table portion of a
// comments upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertCommentsTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "comments" ("id", "data", "synced_at", "content", "file_attachment", "is_deleted", "posted_at", "posted_uid", "reactions", "uids_to_notify")
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "content" = excluded."content", "file_attachment" = excluded."file_attachment", "is_deleted" = excluded."is_deleted", "posted_at" = excluded."posted_at", "posted_uid" = excluded."posted_uid", "reactions" = excluded."reactions", "uids_to_notify" = excluded."uids_to_notify"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "content"),
		lookupFieldValue(obj, "file_attachment"),
		lookupFieldValue(obj, "is_deleted"),
		lookupFieldValue(obj, "posted_at"),
		lookupFieldValue(obj, "posted_uid"),
		lookupFieldValue(obj, "reactions"),
		lookupFieldValue(obj, "uids_to_notify"),
	); err != nil {
		return fmt.Errorf("insert into comments: %w", err)
	}

	return nil
}

// UpsertComments inserts or updates a comments record with domain-specific columns.
func (s *Store) UpsertComments(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling comments: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for comments")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "comments", id, data); err != nil {
		return err
	}
	if err := s.upsertCommentsTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertFoldersTx writes the per-resource domain-table portion of a
// folders upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertFoldersTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "folders" ("id", "data", "synced_at", "child_order", "default_order", "is_deleted", "name", "workspace_id")
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "child_order" = excluded."child_order", "default_order" = excluded."default_order", "is_deleted" = excluded."is_deleted", "name" = excluded."name", "workspace_id" = excluded."workspace_id"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "child_order"),
		lookupFieldValue(obj, "default_order"),
		lookupFieldValue(obj, "is_deleted"),
		lookupFieldValue(obj, "name"),
		lookupFieldValue(obj, "workspace_id"),
	); err != nil {
		return fmt.Errorf("insert into folders: %w", err)
	}

	return nil
}

// UpsertFolders inserts or updates a folders record with domain-specific columns.
func (s *Store) UpsertFolders(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling folders: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for folders")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "folders", id, data); err != nil {
		return err
	}
	if err := s.upsertFoldersTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertLabelsTx writes the per-resource domain-table portion of a
// labels upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertLabelsTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "labels" ("id", "data", "synced_at", "color", "is_favorite", "name", "order")
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "color" = excluded."color", "is_favorite" = excluded."is_favorite", "name" = excluded."name", "order" = excluded."order"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "color"),
		lookupFieldValue(obj, "is_favorite"),
		lookupFieldValue(obj, "name"),
		lookupFieldValue(obj, "order"),
	); err != nil {
		return fmt.Errorf("insert into labels: %w", err)
	}

	return nil
}

// UpsertLabels inserts or updates a labels record with domain-specific columns.
func (s *Store) UpsertLabels(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling labels: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for labels")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "labels", id, data); err != nil {
		return err
	}
	if err := s.upsertLabelsTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertLocationRemindersTx writes the per-resource domain-table portion of a
// location_reminders upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertLocationRemindersTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "location_reminders" ("id", "data", "synced_at", "is_deleted", "item_id", "loc_lat", "loc_long", "loc_trigger", "name", "notify_uid", "project_id", "radius", "type")
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "is_deleted" = excluded."is_deleted", "item_id" = excluded."item_id", "loc_lat" = excluded."loc_lat", "loc_long" = excluded."loc_long", "loc_trigger" = excluded."loc_trigger", "name" = excluded."name", "notify_uid" = excluded."notify_uid", "project_id" = excluded."project_id", "radius" = excluded."radius", "type" = excluded."type"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "is_deleted"),
		lookupFieldValue(obj, "item_id"),
		lookupFieldValue(obj, "loc_lat"),
		lookupFieldValue(obj, "loc_long"),
		lookupFieldValue(obj, "loc_trigger"),
		lookupFieldValue(obj, "name"),
		lookupFieldValue(obj, "notify_uid"),
		lookupFieldValue(obj, "project_id"),
		lookupFieldValue(obj, "radius"),
		lookupFieldValue(obj, "type"),
	); err != nil {
		return fmt.Errorf("insert into location_reminders: %w", err)
	}

	return nil
}

// UpsertLocationReminders inserts or updates a location_reminders record with domain-specific columns.
func (s *Store) UpsertLocationReminders(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling location_reminders: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for location_reminders")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "location-reminders", id, data); err != nil {
		return err
	}
	if err := s.upsertLocationRemindersTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertPaymentsTx writes the per-resource domain-table portion of a
// payments upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertPaymentsTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "payments" ("id", "data", "synced_at", "activation_method", "billing_portal_switch_to_annual_url", "billing_portal_url", "expiration_date", "has_billing_portal", "has_billing_portal_switch_to_annual", "has_switch_legacy_to_current", "invoice_credit_balance", "plan", "plan_price", "status")
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "activation_method" = excluded."activation_method", "billing_portal_switch_to_annual_url" = excluded."billing_portal_switch_to_annual_url", "billing_portal_url" = excluded."billing_portal_url", "expiration_date" = excluded."expiration_date", "has_billing_portal" = excluded."has_billing_portal", "has_billing_portal_switch_to_annual" = excluded."has_billing_portal_switch_to_annual", "has_switch_legacy_to_current" = excluded."has_switch_legacy_to_current", "invoice_credit_balance" = excluded."invoice_credit_balance", "plan" = excluded."plan", "plan_price" = excluded."plan_price", "status" = excluded."status"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "activation_method"),
		lookupFieldValue(obj, "billing_portal_switch_to_annual_url"),
		lookupFieldValue(obj, "billing_portal_url"),
		lookupFieldValue(obj, "expiration_date"),
		lookupFieldValue(obj, "has_billing_portal"),
		lookupFieldValue(obj, "has_billing_portal_switch_to_annual"),
		lookupFieldValue(obj, "has_switch_legacy_to_current"),
		lookupFieldValue(obj, "invoice_credit_balance"),
		lookupFieldValue(obj, "plan"),
		lookupFieldValue(obj, "plan_price"),
		lookupFieldValue(obj, "status"),
	); err != nil {
		return fmt.Errorf("insert into payments: %w", err)
	}

	return nil
}

// UpsertPayments inserts or updates a payments record with domain-specific columns.
func (s *Store) UpsertPayments(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling payments: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for payments")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "payments", id, data); err != nil {
		return err
	}
	if err := s.upsertPaymentsTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertProjectsArchiveTx writes the per-resource domain-table portion of a
// projects_archive upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertProjectsArchiveTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "projects_archive" ("id", "projects_id", "data", "synced_at")
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "projects_id" = excluded."projects_id", "data" = excluded."data", "synced_at" = excluded."synced_at"`,
		id,
		lookupFieldValue(obj, "projects_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert into projects_archive: %w", err)
	}

	return nil
}

// UpsertProjectsArchive inserts or updates a projects_archive record with domain-specific columns.
func (s *Store) UpsertProjectsArchive(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling projects_archive: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for projects_archive")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "projects_archive", id, data); err != nil {
		return err
	}
	if err := s.upsertProjectsArchiveTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertCollaboratorsTx writes the per-resource domain-table portion of a
// collaborators upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertCollaboratorsTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "collaborators" ("id", "projects_id", "data", "synced_at", "parent_id")
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT("id", "projects_id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "parent_id" = excluded."parent_id"`,
		id,
		lookupFieldValue(obj, "projects_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "parent_id"),
	); err != nil {
		return fmt.Errorf("insert into collaborators: %w", err)
	}

	return nil
}

// UpsertCollaborators inserts or updates a collaborators record with domain-specific columns.
func (s *Store) UpsertCollaborators(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling collaborators: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for collaborators")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "collaborators", id, data); err != nil {
		return err
	}
	if err := s.upsertCollaboratorsTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// TombstoneTasksNotIn marks every locally-open task — checked in {0, NULL}
// AND is_deleted in {0, NULL} — whose id is absent from keep as is_deleted=1,
// returning the number of rows tombstoned.
//
// Rationale: the tasks sync reads /api/v1/tasks, which lists ACTIVE tasks only
// and never reports a completion or deletion — a task closed in the app simply
// drops out of the list. Upsert-only sync therefore leaves it behind as a
// phantom "open" row forever (it pollutes agenda/near/focus/priority views).
// After a full, untruncated, anomaly-free active-tasks sync, the set of ids
// seen IS the complete active set, so any locally-open row not in it has left
// the server's active set and is reconciled here.
//
// CONTRACT: keep MUST be the complete active-task id set. A partial or
// truncated sync would wrongly tombstone live tasks — callers gate on a clean,
// non-truncated run and a non-empty keep set before calling.
func (s *Store) TombstoneTasksNotIn(keep map[string]struct{}) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Collect locally-open ids first, then filter against keep in Go. The read
	// cursor is fully drained and closed before any UPDATE so the write does
	// not contend with an open statement on the same connection.
	rows, err := tx.Query(`SELECT id FROM tasks WHERE (checked IS NULL OR checked = 0) AND (is_deleted IS NULL OR is_deleted = 0)`)
	if err != nil {
		return 0, fmt.Errorf("scanning open tasks: %w", err)
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scanning open task id: %w", err)
		}
		if _, ok := keep[id]; !ok {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("reading open task ids: %w", err)
	}
	rows.Close()

	if len(stale) == 0 {
		return 0, nil
	}

	stmt, err := tx.Prepare(`UPDATE tasks SET is_deleted = 1, synced_at = ? WHERE id = ?`)
	if err != nil {
		return 0, fmt.Errorf("preparing tombstone: %w", err)
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range stale {
		if _, err := stmt.Exec(now, id); err != nil {
			return 0, fmt.Errorf("tombstoning task %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing tombstones: %w", err)
	}
	return len(stale), nil
}

// upsertJoinTx writes the per-resource domain-table portion of a
// join upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertJoinTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "join" ("id", "projects_id", "data", "synced_at")
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "projects_id" = excluded."projects_id", "data" = excluded."data", "synced_at" = excluded."synced_at"`,
		id,
		lookupFieldValue(obj, "projects_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert into join: %w", err)
	}

	return nil
}

// UpsertJoin inserts or updates a join record with domain-specific columns.
func (s *Store) UpsertJoin(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling join: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for join")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "join", id, data); err != nil {
		return err
	}
	if err := s.upsertJoinTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertProjectsUnarchiveTx writes the per-resource domain-table portion of a
// projects_unarchive upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertProjectsUnarchiveTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "projects_unarchive" ("id", "projects_id", "data", "synced_at")
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "projects_id" = excluded."projects_id", "data" = excluded."data", "synced_at" = excluded."synced_at"`,
		id,
		lookupFieldValue(obj, "projects_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert into projects_unarchive: %w", err)
	}

	return nil
}

// UpsertProjectsUnarchive inserts or updates a projects_unarchive record with domain-specific columns.
func (s *Store) UpsertProjectsUnarchive(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling projects_unarchive: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for projects_unarchive")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "projects_unarchive", id, data); err != nil {
		return err
	}
	if err := s.upsertProjectsUnarchiveTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertRemindersTx writes the per-resource domain-table portion of a
// reminders upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertRemindersTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "reminders" ("id", "data", "synced_at", "due", "is_deleted", "is_urgent", "item_id", "minute_offset", "notify_uid", "type")
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "due" = excluded."due", "is_deleted" = excluded."is_deleted", "is_urgent" = excluded."is_urgent", "item_id" = excluded."item_id", "minute_offset" = excluded."minute_offset", "notify_uid" = excluded."notify_uid", "type" = excluded."type"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "due"),
		lookupFieldValue(obj, "is_deleted"),
		lookupFieldValue(obj, "is_urgent"),
		lookupFieldValue(obj, "item_id"),
		lookupFieldValue(obj, "minute_offset"),
		lookupFieldValue(obj, "notify_uid"),
		lookupFieldValue(obj, "type"),
	); err != nil {
		return fmt.Errorf("insert into reminders: %w", err)
	}

	return nil
}

// UpsertReminders inserts or updates a reminders record with domain-specific columns.
func (s *Store) UpsertReminders(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling reminders: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for reminders")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "reminders", id, data); err != nil {
		return err
	}
	if err := s.upsertRemindersTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertSectionsTx writes the per-resource domain-table portion of a
// sections upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertSectionsTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "sections" ("id", "data", "synced_at", "added_at", "archived_at", "is_archived", "is_collapsed", "is_deleted", "name", "project_id", "section_order", "updated_at", "user_id")
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "added_at" = excluded."added_at", "archived_at" = excluded."archived_at", "is_archived" = excluded."is_archived", "is_collapsed" = excluded."is_collapsed", "is_deleted" = excluded."is_deleted", "name" = excluded."name", "project_id" = excluded."project_id", "section_order" = excluded."section_order", "updated_at" = excluded."updated_at", "user_id" = excluded."user_id"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "added_at"),
		lookupFieldValue(obj, "archived_at"),
		lookupFieldValue(obj, "is_archived"),
		lookupFieldValue(obj, "is_collapsed"),
		lookupFieldValue(obj, "is_deleted"),
		lookupFieldValue(obj, "name"),
		lookupFieldValue(obj, "project_id"),
		lookupFieldValue(obj, "section_order"),
		lookupFieldValue(obj, "updated_at"),
		lookupFieldValue(obj, "user_id"),
	); err != nil {
		return fmt.Errorf("insert into sections: %w", err)
	}

	return nil
}

// UpsertSections inserts or updates a sections record with domain-specific columns.
func (s *Store) UpsertSections(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling sections: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for sections")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "sections", id, data); err != nil {
		return err
	}
	if err := s.upsertSectionsTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertSectionsArchiveTx writes the per-resource domain-table portion of a
// sections_archive upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertSectionsArchiveTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "sections_archive" ("id", "sections_id", "data", "synced_at")
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "sections_id" = excluded."sections_id", "data" = excluded."data", "synced_at" = excluded."synced_at"`,
		id,
		lookupFieldValue(obj, "sections_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert into sections_archive: %w", err)
	}

	return nil
}

// UpsertSectionsArchive inserts or updates a sections_archive record with domain-specific columns.
func (s *Store) UpsertSectionsArchive(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling sections_archive: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for sections_archive")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "sections_archive", id, data); err != nil {
		return err
	}
	if err := s.upsertSectionsArchiveTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertSectionsUnarchiveTx writes the per-resource domain-table portion of a
// sections_unarchive upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertSectionsUnarchiveTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "sections_unarchive" ("id", "sections_id", "data", "synced_at")
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "sections_id" = excluded."sections_id", "data" = excluded."data", "synced_at" = excluded."synced_at"`,
		id,
		lookupFieldValue(obj, "sections_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert into sections_unarchive: %w", err)
	}

	return nil
}

// UpsertSectionsUnarchive inserts or updates a sections_unarchive record with domain-specific columns.
func (s *Store) UpsertSectionsUnarchive(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling sections_unarchive: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for sections_unarchive")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "sections_unarchive", id, data); err != nil {
		return err
	}
	if err := s.upsertSectionsUnarchiveTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertTasksTx writes the per-resource domain-table portion of a
// tasks upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertTasksTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "tasks" ("id", "data", "synced_at", "next_cursor", "completed_count", "karma", "karma_last_update", "karma_trend", "added_at", "added_by_uid", "assigned_by_uid", "checked", "child_order", "completed_at", "completed_by_uid", "content", "day_order", "deadline", "description", "due", "duration", "is_collapsed", "is_deleted", "note_count", "parent_id", "priority", "project_id", "responsible_uid", "section_id", "updated_at", "user_id")
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "next_cursor" = excluded."next_cursor", "completed_count" = excluded."completed_count", "karma" = excluded."karma", "karma_last_update" = excluded."karma_last_update", "karma_trend" = excluded."karma_trend", "added_at" = excluded."added_at", "added_by_uid" = excluded."added_by_uid", "assigned_by_uid" = excluded."assigned_by_uid", "checked" = excluded."checked", "child_order" = excluded."child_order", "completed_at" = excluded."completed_at", "completed_by_uid" = excluded."completed_by_uid", "content" = excluded."content", "day_order" = excluded."day_order", "deadline" = excluded."deadline", "description" = excluded."description", "due" = excluded."due", "duration" = excluded."duration", "is_collapsed" = excluded."is_collapsed", "is_deleted" = excluded."is_deleted", "note_count" = excluded."note_count", "parent_id" = excluded."parent_id", "priority" = excluded."priority", "project_id" = excluded."project_id", "responsible_uid" = excluded."responsible_uid", "section_id" = excluded."section_id", "updated_at" = excluded."updated_at", "user_id" = excluded."user_id"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "next_cursor"),
		lookupFieldValue(obj, "completed_count"),
		lookupFieldValue(obj, "karma"),
		lookupFieldValue(obj, "karma_last_update"),
		lookupFieldValue(obj, "karma_trend"),
		lookupFieldValue(obj, "added_at"),
		lookupFieldValue(obj, "added_by_uid"),
		lookupFieldValue(obj, "assigned_by_uid"),
		lookupFieldValue(obj, "checked"),
		lookupFieldValue(obj, "child_order"),
		lookupFieldValue(obj, "completed_at"),
		lookupFieldValue(obj, "completed_by_uid"),
		lookupFieldValue(obj, "content"),
		lookupFieldValue(obj, "day_order"),
		lookupFieldValue(obj, "deadline"),
		lookupFieldValue(obj, "description"),
		lookupFieldValue(obj, "due"),
		lookupFieldValue(obj, "duration"),
		lookupFieldValue(obj, "is_collapsed"),
		lookupFieldValue(obj, "is_deleted"),
		lookupFieldValue(obj, "note_count"),
		lookupFieldValue(obj, "parent_id"),
		lookupFieldValue(obj, "priority"),
		lookupFieldValue(obj, "project_id"),
		lookupFieldValue(obj, "responsible_uid"),
		lookupFieldValue(obj, "section_id"),
		lookupFieldValue(obj, "updated_at"),
		lookupFieldValue(obj, "user_id"),
	); err != nil {
		return fmt.Errorf("insert into tasks: %w", err)
	}

	return nil
}

// UpsertTasks inserts or updates a tasks record with domain-specific columns.
func (s *Store) UpsertTasks(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling tasks: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for tasks")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "tasks", id, data); err != nil {
		return err
	}
	if err := s.upsertTasksTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertCloseTx writes the per-resource domain-table portion of a
// close upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertCloseTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "close" ("id", "tasks_id", "data", "synced_at")
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "tasks_id" = excluded."tasks_id", "data" = excluded."data", "synced_at" = excluded."synced_at"`,
		id,
		lookupFieldValue(obj, "tasks_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert into close: %w", err)
	}

	return nil
}

// UpsertClose inserts or updates a close record with domain-specific columns.
func (s *Store) UpsertClose(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling close: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for close")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "close", id, data); err != nil {
		return err
	}
	if err := s.upsertCloseTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertMoveTx writes the per-resource domain-table portion of a
// move upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertMoveTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "move" ("id", "tasks_id", "data", "synced_at")
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "tasks_id" = excluded."tasks_id", "data" = excluded."data", "synced_at" = excluded."synced_at"`,
		id,
		lookupFieldValue(obj, "tasks_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert into move: %w", err)
	}

	return nil
}

// UpsertMove inserts or updates a move record with domain-specific columns.
func (s *Store) UpsertMove(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling move: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for move")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "move", id, data); err != nil {
		return err
	}
	if err := s.upsertMoveTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertReopenTx writes the per-resource domain-table portion of a
// reopen upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertReopenTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "reopen" ("id", "tasks_id", "data", "synced_at")
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "tasks_id" = excluded."tasks_id", "data" = excluded."data", "synced_at" = excluded."synced_at"`,
		id,
		lookupFieldValue(obj, "tasks_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert into reopen: %w", err)
	}

	return nil
}

// UpsertReopen inserts or updates a reopen record with domain-specific columns.
func (s *Store) UpsertReopen(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling reopen: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for reopen")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "reopen", id, data); err != nil {
		return err
	}
	if err := s.upsertReopenTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertTemplatesTx writes the per-resource domain-table portion of a
// templates upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertTemplatesTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "templates" ("id", "data", "synced_at", "project_id", "status", "template_type")
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "project_id" = excluded."project_id", "status" = excluded."status", "template_type" = excluded."template_type"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "project_id"),
		lookupFieldValue(obj, "status"),
		lookupFieldValue(obj, "template_type"),
	); err != nil {
		return fmt.Errorf("insert into templates: %w", err)
	}

	return nil
}

// UpsertTemplates inserts or updates a templates record with domain-specific columns.
func (s *Store) UpsertTemplates(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling templates: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for templates")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "templates", id, data); err != nil {
		return err
	}
	if err := s.upsertTemplatesTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertUploadsTx writes the per-resource domain-table portion of a
// uploads upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertUploadsTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "uploads" ("id", "data", "synced_at", "file_name", "file_size", "file_type", "file_url", "image", "image_height", "image_width", "resource_type", "upload_state")
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "file_name" = excluded."file_name", "file_size" = excluded."file_size", "file_type" = excluded."file_type", "file_url" = excluded."file_url", "image" = excluded."image", "image_height" = excluded."image_height", "image_width" = excluded."image_width", "resource_type" = excluded."resource_type", "upload_state" = excluded."upload_state"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "file_name"),
		lookupFieldValue(obj, "file_size"),
		lookupFieldValue(obj, "file_type"),
		lookupFieldValue(obj, "file_url"),
		lookupFieldValue(obj, "image"),
		lookupFieldValue(obj, "image_height"),
		lookupFieldValue(obj, "image_width"),
		lookupFieldValue(obj, "resource_type"),
		lookupFieldValue(obj, "upload_state"),
	); err != nil {
		return fmt.Errorf("insert into uploads: %w", err)
	}

	return nil
}

// UpsertUploads inserts or updates a uploads record with domain-specific columns.
func (s *Store) UpsertUploads(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling uploads: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for uploads")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "uploads", id, data); err != nil {
		return err
	}
	if err := s.upsertUploadsTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertWorkspacesTx writes the per-resource domain-table portion of a
// workspaces upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertWorkspacesTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "workspaces" ("id", "data", "synced_at", "inviter_id", "is_existing_user", "role", "user_email", "workspace_id")
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "data" = excluded."data", "synced_at" = excluded."synced_at", "inviter_id" = excluded."inviter_id", "is_existing_user" = excluded."is_existing_user", "role" = excluded."role", "user_email" = excluded."user_email", "workspace_id" = excluded."workspace_id"`,
		id,
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "inviter_id"),
		lookupFieldValue(obj, "is_existing_user"),
		lookupFieldValue(obj, "role"),
		lookupFieldValue(obj, "user_email"),
		lookupFieldValue(obj, "workspace_id"),
	); err != nil {
		return fmt.Errorf("insert into workspaces: %w", err)
	}

	return nil
}

// UpsertWorkspaces inserts or updates a workspaces record with domain-specific columns.
func (s *Store) UpsertWorkspaces(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling workspaces: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for workspaces")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "workspaces", id, data); err != nil {
		return err
	}
	if err := s.upsertWorkspacesTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertWorkspacesProjectsTx writes the per-resource domain-table portion of a
// workspaces_projects upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertWorkspacesProjectsTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "workspaces_projects" ("id", "workspaces_id", "data", "synced_at", "parent_id")
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "workspaces_id" = excluded."workspaces_id", "data" = excluded."data", "synced_at" = excluded."synced_at", "parent_id" = excluded."parent_id"`,
		id,
		lookupFieldValue(obj, "workspaces_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
		lookupFieldValue(obj, "parent_id"),
	); err != nil {
		return fmt.Errorf("insert into workspaces_projects: %w", err)
	}

	return nil
}

// UpsertWorkspacesProjects inserts or updates a workspaces_projects record with domain-specific columns.
func (s *Store) UpsertWorkspacesProjects(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling workspaces_projects: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for workspaces_projects")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "workspaces_projects", id, data); err != nil {
		return err
	}
	if err := s.upsertWorkspacesProjectsTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// upsertUsersTx writes the per-resource domain-table portion of a
// users upsert inside an existing transaction. The caller is
// responsible for the generic resources insert (via upsertGenericResourceTx)
// and for committing the tx. Splitting this out lets UpsertBatch dispatch
// domain inserts per item without opening a per-item transaction.
func (s *Store) upsertUsersTx(tx *sql.Tx, id string, obj map[string]any, data json.RawMessage) error {
	if _, err := tx.Exec(
		`INSERT INTO "users" ("id", "workspaces_id", "data", "synced_at")
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT("id") DO UPDATE SET "workspaces_id" = excluded."workspaces_id", "data" = excluded."data", "synced_at" = excluded."synced_at"`,
		id,
		lookupFieldValue(obj, "workspaces_id"),
		string(data),
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert into users: %w", err)
	}

	return nil
}

// UpsertUsers inserts or updates a users record with domain-specific columns.
func (s *Store) UpsertUsers(data json.RawMessage) error {
	obj, err := DecodeJSONObject(data)
	if err != nil {
		return fmt.Errorf("unmarshaling users: %w", err)
	}

	id := extractObjectID(obj)
	if id == "" {
		return fmt.Errorf("missing id for users")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.upsertGenericResourceTx(tx, "users", id, data); err != nil {
		return err
	}
	if err := s.upsertUsersTx(tx, id, obj, data); err != nil {
		return err
	}

	return tx.Commit()
}

// resourceIDFieldOverrides projects per-resource IDField (set by the profiler
// from x-resource-id or response-schema fallback) into a runtime lookup map.
// UpsertBatch consults this first so the templated path wins over the
// generic fallback list. Empty when no resource declared an override; the
// runtime fallback list still applies.
//
// Includes both flat resources and dependent (parent-child) resources so a
// child path-item annotated with x-resource-id resolves the same as a flat
// path-item.
var resourceIDFieldOverrides = map[string]string{
	"activities":          "id",
	"backups":             "version",
	"collaborators":       "id",
	"comments":            "id",
	"labels":              "id",
	"location-reminders":  "id",
	"reminders":           "id",
	"sections":            "id",
	"tasks":               "id",
	"workspaces-users":    "workspace_id",
	"workspaces_projects": "project_id",
}

// genericIDFieldFallbacks is the runtime safety net for resources that did
// NOT receive a templated IDField. API-specific names belong in spec
// annotations (x-resource-id), not this list. Order matters: vendor
// identifier names (gid, sid, uid, uuid, guid) take precedence over `name`
// so APIs like Asana (gid) and Twilio (sid) don't fall through to a display
// field and upsert on names — see #1394.
var genericIDFieldFallbacks = []string{"id", "ID", "gid", "sid", "uid", "uuid", "guid", "name", "slug", "key", "code"}

// ExtractResourceID resolves the primary key UpsertBatch would use for a
// resource item. Callers that need to gate best-effort writes can use this to
// avoid passing non-entity envelopes into the batch path.
func ExtractResourceID(resourceType string, obj map[string]any) string {
	if override, ok := resourceIDFieldOverrides[resourceType]; ok && override != "" {
		if v := lookupFieldValue(obj, override); v != nil {
			s := ResourceIDString(v)
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	for _, key := range genericIDFieldFallbacks {
		if v := lookupFieldValue(obj, key); v != nil {
			s := ResourceIDString(v)
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

// UpsertBatch inserts or replaces multiple records in a single transaction
// and returns (stored, extractFailures, err). stored counts rows landed in
// the generic resources table; extractFailures counts items that survived
// JSON unmarshal but had no extractable primary key (templated IDField AND
// generic fallback both missed). callers (sync.go.tmpl) compare these
// against len(items) to emit the per-item primary_key_unresolved warning
// and the F4b stored_count_zero_after_extraction probe.
//
// For resource types that have a domain-specific typed table, the per-item
// generic insert is followed by a dispatch to the matching upsert<Pascal>Tx
// inside the same transaction. Without that dispatch, paginated syncs would
// only populate the generic resources table — typed tables (and indexed
// columns like parent_id added by dependent-resource sync) would stay empty.
//
// Each typed-table dispatch runs inside a per-item SAVEPOINT so a constraint
// failure in the typed insert (e.g. NOT NULL parent FK when the generator
// didn't populate the parent path placeholder) rolls back only that typed
// upsert. The generic resources row inserted just above it survives the
// rollback, so successful API fetches never strand in memory because one
// downstream typed table is misconfigured. Failures are surfaced via a
// trailing stderr warning rather than aborting the batch.
func (s *Store) UpsertBatch(resourceType string, items []json.RawMessage) (int, int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("starting batch transaction: %w", err)
	}
	defer tx.Rollback()

	var stored, skippedCount, extractFailures, typedFailures int
	for i, item := range items {
		obj, err := DecodeJSONObject(item)
		if err != nil {
			skippedCount++
			continue
		}
		// Templated IDField wins; generic fallback list runs second when
		// the override is empty OR the override field is absent on this
		// particular item (response shape mismatches happen even when the
		// spec declares x-resource-id).
		id := ExtractResourceID(resourceType, obj)
		if id == "" {
			skippedCount++
			extractFailures++
			continue
		}

		if err := s.upsertGenericResourceTx(tx, resourceType, id, item); err != nil {
			// Return the running stored count rather than zero so callers
			// inspecting partial progress on failure see what already
			// landed in earlier loop iterations.
			return stored, extractFailures, fmt.Errorf("upserting %s/%s: %w", resourceType, id, err)
		}
		stored++

		savepoint := fmt.Sprintf("pp_typed_%d", i)
		if _, err := tx.Exec("SAVEPOINT " + savepoint); err != nil {
			return stored, extractFailures, fmt.Errorf("savepoint begin for %s/%s: %w", resourceType, id, err)
		}

		var typedErr error
		switch resourceType {
		case "comments":
			typedErr = s.upsertCommentsTx(tx, id, obj, item)
		case "folders":
			typedErr = s.upsertFoldersTx(tx, id, obj, item)
		case "labels":
			typedErr = s.upsertLabelsTx(tx, id, obj, item)
		case "location-reminders":
			typedErr = s.upsertLocationRemindersTx(tx, id, obj, item)
		case "payments":
			typedErr = s.upsertPaymentsTx(tx, id, obj, item)
		case "projects_archive":
			typedErr = s.upsertProjectsArchiveTx(tx, id, obj, item)
		case "collaborators":
			typedErr = s.upsertCollaboratorsTx(tx, id, obj, item)
		case "join":
			typedErr = s.upsertJoinTx(tx, id, obj, item)
		case "projects_unarchive":
			typedErr = s.upsertProjectsUnarchiveTx(tx, id, obj, item)
		case "reminders":
			typedErr = s.upsertRemindersTx(tx, id, obj, item)
		case "sections":
			typedErr = s.upsertSectionsTx(tx, id, obj, item)
		case "sections_archive":
			typedErr = s.upsertSectionsArchiveTx(tx, id, obj, item)
		case "sections_unarchive":
			typedErr = s.upsertSectionsUnarchiveTx(tx, id, obj, item)
		case "tasks":
			typedErr = s.upsertTasksTx(tx, id, obj, item)
		case "close":
			typedErr = s.upsertCloseTx(tx, id, obj, item)
		case "move":
			typedErr = s.upsertMoveTx(tx, id, obj, item)
		case "reopen":
			typedErr = s.upsertReopenTx(tx, id, obj, item)
		case "templates":
			typedErr = s.upsertTemplatesTx(tx, id, obj, item)
		case "uploads":
			typedErr = s.upsertUploadsTx(tx, id, obj, item)
		case "workspaces":
			typedErr = s.upsertWorkspacesTx(tx, id, obj, item)
		case "workspaces_projects":
			typedErr = s.upsertWorkspacesProjectsTx(tx, id, obj, item)
		case "users":
			typedErr = s.upsertUsersTx(tx, id, obj, item)
		}

		if typedErr != nil {
			if _, rbErr := tx.Exec("ROLLBACK TO SAVEPOINT " + savepoint); rbErr != nil {
				return stored, extractFailures, fmt.Errorf("rollback to savepoint for %s/%s (typed err: %v): %w", resourceType, id, typedErr, rbErr)
			}
			if _, relErr := tx.Exec("RELEASE SAVEPOINT " + savepoint); relErr != nil {
				return stored, extractFailures, fmt.Errorf("release savepoint after rollback for %s/%s: %w", resourceType, id, relErr)
			}
			typedFailures++
			continue
		}
		if _, err := tx.Exec("RELEASE SAVEPOINT " + savepoint); err != nil {
			return stored, extractFailures, fmt.Errorf("release savepoint for %s/%s: %w", resourceType, id, err)
		}
	}

	// Warn when most items in a batch lack an extractable ID — this likely
	// means the API uses a primary key field we don't recognize yet.
	if skippedCount > 0 && len(items) > 0 && skippedCount*2 > len(items) {
		fmt.Fprintf(os.Stderr, "warning: %d/%d %s items returned but not cached locally (no extractable ID field; offline lookup against these rows will be incomplete; live queries unaffected)\n", skippedCount, len(items), resourceType)
	}
	// Surface typed-table failures without aborting the batch. Generic rows
	// already committed; only the typed projection failed.
	if typedFailures > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d/%d %s items: typed-table upsert failed; generic resources rows preserved\n", typedFailures, len(items), resourceType)
	}

	if err := tx.Commit(); err != nil {
		return 0, extractFailures, err
	}
	return stored, extractFailures, nil
}

// SearchTasks searches the tasks_fts index with optional filters.
func (s *Store) SearchTasks(query string, limit int) ([]json.RawMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT t.data FROM "tasks" t
		 JOIN "tasks_fts" ON "tasks_fts".rowid = t.rowid
		 WHERE "tasks_fts" MATCH ?
		 ORDER BY rank LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		results = append(results, json.RawMessage(data))
	}
	return results, rows.Err()
}

func (s *Store) SaveSyncState(resourceType, cursor string, count int) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO sync_state (resource_type, last_cursor, last_synced_at, total_count)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(resource_type) DO UPDATE SET last_cursor = excluded.last_cursor,
		 last_synced_at = excluded.last_synced_at, total_count = excluded.total_count`,
		resourceType, cursor, time.Now().UTC().Format(time.RFC3339), count,
	)
	return err
}

func (s *Store) GetSyncState(resourceType string) (cursor string, lastSynced time.Time, count int, err error) {
	err = s.db.QueryRow(
		`SELECT last_cursor, last_synced_at, total_count FROM sync_state WHERE resource_type = ?`,
		resourceType,
	).Scan(&cursor, &lastSynced, &count)
	if err == sql.ErrNoRows {
		return "", time.Time{}, 0, nil
	}
	return
}

// SaveSyncCursor stores the pagination cursor for a resource type.
func (s *Store) SaveSyncCursor(resourceType, cursor string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO sync_state (resource_type, last_cursor, last_synced_at, total_count)
		 VALUES (?, ?, CURRENT_TIMESTAMP, 0)
		 ON CONFLICT(resource_type) DO UPDATE SET last_cursor = ?, last_synced_at = CURRENT_TIMESTAMP`,
		resourceType, cursor, cursor,
	)
	return err
}

// GetSyncCursor returns the last pagination cursor for a resource type.
func (s *Store) GetSyncCursor(resourceType string) string {
	var cursor sql.NullString
	s.db.QueryRow("SELECT last_cursor FROM sync_state WHERE resource_type = ?", resourceType).Scan(&cursor)
	if cursor.Valid {
		return cursor.String
	}
	return ""
}

// ListIDs returns all IDs from a resource's domain table, or from the generic
// resources table if no domain table exists. Used by dependent sync to iterate parents.
//
// resourceType is never interpolated into SQL directly. We resolve it to a real
// table name via a parameterized sqlite_master lookup; only that trusted name is
// substituted (double-quoted) into the SELECT. Callers may pass any string.
func (s *Store) ListIDs(resourceType string) ([]string, error) {
	var table string
	err := s.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
		resourceType,
	).Scan(&table)
	var rows *sql.Rows
	if err == nil && table != "" {
		rows, err = s.db.Query(fmt.Sprintf(`SELECT id FROM "%s"`, strings.ReplaceAll(table, `"`, `""`)))
	}
	if err != nil || table == "" {
		// Fall back to generic resources table
		rows, err = s.db.Query("SELECT id FROM resources WHERE resource_type = ?", resourceType)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListField returns values of a named field from a resource's domain table,
// or from the generic resources table via json_extract when no typed column
// exists. Used by dependent sync to iterate parents when a spec-declared
// walker extracts a non-PK field (Endpoint.Walker.KeyField in the upstream
// spec or auth metadata) for the child path's placeholder.
//
// Defense in depth: field is validated against validIdentifierRE at entry
// — the regex pins it to SQL-safe identifier shape covering both the
// typed-column primary path AND the json_extract fallback (where
// pragma_table_info validation would never run if the parent's domain
// table doesn't exist yet). resourceType is never interpolated into SQL
// directly; we resolve it to a real table name via a parameterized
// sqlite_master lookup. Only validated names are substituted
// (double-quoted) into the SELECT. Mirrors ListIDs's defense pattern so
// callers may pass any string.
func (s *Store) ListField(resourceType, field string) ([]string, error) {
	if !validIdentifierRE.MatchString(field) {
		return nil, fmt.Errorf("ListField: invalid field name %q (must match %s)", field, validIdentifierRE.String())
	}
	var table string
	err := s.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
		resourceType,
	).Scan(&table)
	var rows *sql.Rows
	if err == nil && table != "" {
		// Validate the column exists on the resolved table before splicing
		// it into the SELECT. pragma_table_info is parameterizable.
		var colName string
		colErr := s.db.QueryRow(
			`SELECT name FROM pragma_table_info(?) WHERE name=?`,
			table, field,
		).Scan(&colName)
		if colErr == nil && colName != "" {
			qTable := strings.ReplaceAll(table, `"`, `""`)
			qCol := strings.ReplaceAll(colName, `"`, `""`)
			// DISTINCT: callers iterate the returned values as parent keys
			// for child-resource fan-out. Multiple parent rows sharing a
			// key_field value (legal for non-PK fields) would otherwise
			// cause the child endpoint to be fetched once per duplicate row.
			rows, err = s.db.Query(fmt.Sprintf(
				`SELECT DISTINCT "%s" FROM "%s" WHERE "%s" IS NOT NULL AND "%s" != ''`,
				qCol, qTable, qCol, qCol,
			))
		} else {
			err = colErr
		}
	}
	if err != nil || rows == nil {
		// Fall back to generic resources table via json_extract. Path is
		// Sprintf'd into the SQL string (matches ResolveByName below).
		// DISTINCT for the same reason as the typed-column path above.
		fallback := fmt.Sprintf(
			`SELECT DISTINCT json_extract(data, '$.%s') FROM resources WHERE resource_type = ? AND json_extract(data, '$.%s') IS NOT NULL`,
			field, field,
		)
		rows, err = s.db.Query(fallback, resourceType)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err == nil && v.Valid && v.String != "" {
			values = append(values, v.String)
		}
	}
	return values, rows.Err()
}

// ListFieldSets returns row-correlated values from the generic resources
// table. Dependent sync uses this for multi-placeholder paths where values
// such as owner/repo or server/webapp must stay paired per parent row.
func (s *Store) ListFieldSets(resourceType string, fields []string) ([]map[string]string, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	for _, field := range fields {
		if !validIdentifierRE.MatchString(field) {
			return nil, fmt.Errorf("ListFieldSets: invalid field name %q (must match %s)", field, validIdentifierRE.String())
		}
	}

	rows, err := s.db.Query(`SELECT id, data FROM resources WHERE resource_type = ?`, resourceType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]string
	seenRows := map[string]bool{}
	for rows.Next() {
		var id string
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		var obj map[string]any
		if len(data) > 0 {
			var err error
			obj, err = DecodeJSONObject(data)
			if err != nil {
				return nil, fmt.Errorf("decode %s parent row %s: %w", resourceType, id, err)
			}
		}
		values := make(map[string]string, len(fields))
		complete := true
		for _, field := range fields {
			var value any
			if field == "id" {
				value = id
			} else {
				value = LookupFieldValue(obj, field)
			}
			valueString := ResourceIDString(value)
			if value == nil || valueString == "" {
				complete = false
				break
			}
			values[field] = valueString
		}
		if complete {
			keyParts := make([]string, 0, len(fields))
			for _, field := range fields {
				keyParts = append(keyParts, values[field])
			}
			key := strings.Join(keyParts, "\x00")
			if seenRows[key] {
				continue
			}
			seenRows[key] = true
			out = append(out, values)
		}
	}
	return out, rows.Err()
}

// GetLastSyncedAt returns the last sync timestamp for a resource type.
func (s *Store) GetLastSyncedAt(resourceType string) string {
	var ts sql.NullString
	s.db.QueryRow("SELECT last_synced_at FROM sync_state WHERE resource_type = ?", resourceType).Scan(&ts)
	if ts.Valid {
		return ts.String
	}
	return ""
}

// ClearSyncCursors resets all sync state for a full resync.
func (s *Store) ClearSyncCursors() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.db.Exec("DELETE FROM sync_state")
	return err
}

// Query executes a raw SQL query and returns the rows.
// Used by workflow commands that need custom queries against the local store.
func (s *Store) Query(query string, args ...any) (*sql.Rows, error) {
	return s.db.Query(query, args...)
}

func (s *Store) Count(resourceType string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM resources WHERE resource_type = ?`,
		resourceType,
	).Scan(&count)
	return count, err
}

func (s *Store) Status() (map[string]int, error) {
	rows, err := s.db.Query(
		`SELECT resource_type, COUNT(*) FROM resources GROUP BY resource_type ORDER BY resource_type`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	status := make(map[string]int)
	for rows.Next() {
		var rt string
		var count int
		if err := rows.Scan(&rt, &count); err != nil {
			return nil, err
		}
		status[rt] = count
	}
	return status, rows.Err()
}

// ResolveByName resolves a human-readable name to a UUID from synced data.
// If the input is already a UUID, it is returned as-is.
// matchFields are JSON field names to search against (e.g., "name", "key", "email").
//
// json_extract path components cannot be bound as SQL parameters, so each
// field is validated against validIdentifierRE before being spliced into
// the query.
func (s *Store) ResolveByName(resourceType string, input string, matchFields ...string) (string, error) {
	if IsUUID(input) {
		return input, nil
	}

	var matches []string
	for _, field := range matchFields {
		if !validIdentifierRE.MatchString(field) {
			continue
		}
		query := fmt.Sprintf(
			`SELECT id FROM resources WHERE resource_type = ? AND LOWER(json_extract(data, '$.%s')) = LOWER(?)`,
			field,
		)
		rows, err := s.db.Query(query, resourceType, input)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				// Deduplicate
				found := false
				for _, m := range matches {
					if m == id {
						found = true
						break
					}
				}
				if !found {
					matches = append(matches, id)
				}
			}
		}
		rows.Close()
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%s %q not found in local store. Run 'sync' first, or use the UUID directly", resourceType, input)
	case 1:
		return matches[0], nil
	default:
		hint := matches[0]
		if len(matches) > 5 {
			hint = strings.Join(matches[:5], ", ") + "..."
		} else {
			hint = strings.Join(matches, ", ")
		}
		return "", fmt.Errorf("ambiguous: %q matches %d %s entries (%s). Use the exact UUID instead", input, len(matches), resourceType, hint)
	}
}
