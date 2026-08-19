package execution

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-go-golems/flowkit/internal/digest"
	"github.com/go-go-golems/flowkit/internal/jsonutil"
	_ "github.com/go-sql-driver/mysql"
)

// mysqlMediumTextMax is the byte capacity of a MEDIUMTEXT column (2^24 - 1).
// MaxEntryBytes is capped at this so an envelope that passes the
// application-side size check can always be stored.
const (
	mysqlMediumTextMax    = 1<<24 - 1
	mysqlCacheSchemaV1    = int64(1)
	mysqlSchemaVersionTbl = "flowkit_schema_version"
)

// mysqlIdentifier restricts configurable identifiers before they are quoted.
// Quoting remains mandatory because syntactically valid identifiers can also
// be MySQL reserved words such as "select".
var mysqlIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]{0,63}$`)

// MySQLCacheOptions configures a MySQL-backed execution.Cache.
//
// DSN is a go-sql-driver/mysql DSN, e.g.
//
//	"user:pass@tcp(127.0.0.1:3306)/coinvault_cache_dev?parseTime=true"
//
// Empty DSN is rejected; callers select between MySQL and FileCache at the
// wiring layer (see OpenCache). TableName defaults to "cache_entries"; tests
// pass a unique table name for isolation. Pool settings default to a bounded
// pool suitable for the bursty, parallel MapCached hot path.
type MySQLCacheOptions struct {
	DSN             string
	TableName       string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	MaxEntryBytes   int64
}

// MySQLCache stores one validated result envelope per key in a single MySQL
// table, keyed by Key.Digest(). It is a drop-in replacement for FileCache: the
// value_json column holds the exact envelope FileCache writes to disk, so Load
// performs the same schema / key / value-digest validation and fails closed on
// corruption. MySQL's commit is the atomic publish, removing FileCache's
// temp->fsync->rename hazard and the task-died-before-sync window.
type MySQLCache struct {
	db            *sql.DB
	table         string
	maxEntryBytes int64
}

var _ Cache = (*MySQLCache)(nil)

// NewMySQLCache opens the connection pool, creates the cache table if it does
// not exist, and returns a Cache backed by MySQL. The returned cache owns the
// connection pool; Close releases it.
func NewMySQLCache(ctx context.Context, opts MySQLCacheOptions) (*MySQLCache, error) {
	if strings.TrimSpace(opts.DSN) == "" {
		return nil, fmt.Errorf("mysql cache dsn is required")
	}
	table := strings.TrimSpace(opts.TableName)
	if table == "" {
		table = "cache_entries"
	}
	if !mysqlIdentifier.MatchString(table) {
		return nil, fmt.Errorf("mysql cache table name %q is not a valid MySQL identifier (letters, digits, underscore, $; 1-64 chars)", table)
	}
	maxEntryBytes := opts.MaxEntryBytes
	if maxEntryBytes == 0 {
		maxEntryBytes = mysqlMediumTextMax
	}
	if maxEntryBytes < 1 {
		return nil, fmt.Errorf("mysql cache maximum entry bytes must be positive")
	}
	if maxEntryBytes > mysqlMediumTextMax {
		return nil, fmt.Errorf("mysql cache maximum entry bytes %d exceeds MEDIUMTEXT capacity %d; use a smaller MaxEntryBytes or a column type that can hold it", maxEntryBytes, mysqlMediumTextMax)
	}

	db, err := openMySQLPool(opts.DSN, opts.MaxOpenConns, opts.MaxIdleConns, opts.ConnMaxLifetime, opts.ConnMaxIdleTime)
	if err != nil {
		return nil, fmt.Errorf("open mysql cache: %w", err)
	}
	cache := &MySQLCache{db: db, table: table, maxEntryBytes: maxEntryBytes}
	if err := cache.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("mysql cache migrate: %w", err)
	}
	return cache, nil
}

// Close releases the underlying connection pool.
func (c *MySQLCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *MySQLCache) migrate(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := c.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var database string
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return fmt.Errorf("read selected database: %w", err)
	}
	lockName := mysqlCacheMigrationLock(database)
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 30)", lockName).Scan(&acquired); err != nil {
		return fmt.Errorf("acquire schema lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return fmt.Errorf("acquire schema lock %q: timed out", lockName)
	}
	defer func() {
		var released sql.NullInt64
		_ = conn.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", lockName).Scan(&released)
	}()

	versionTableExists, err := mysqlTableExists(ctx, conn, mysqlSchemaVersionTbl)
	if err != nil {
		return fmt.Errorf("inspect schema version table: %w", err)
	}
	cacheTableExists, err := mysqlTableExists(ctx, conn, c.table)
	if err != nil {
		return fmt.Errorf("inspect cache table: %w", err)
	}
	if !versionTableExists {
		if cacheTableExists {
			return fmt.Errorf("mysql cache: unversioned prototype table %q detected; recreate it or migrate it explicitly", c.table)
		}
		if _, err := conn.ExecContext(ctx, `CREATE TABLE flowkit_schema_version (
  component VARBINARY(128) NOT NULL PRIMARY KEY,
  schema_version BIGINT NOT NULL
) ENGINE=InnoDB`); err != nil {
			return fmt.Errorf("create schema version table: %w", err)
		}
	}

	component := mysqlCacheSchemaComponent(c.table)
	var version int64
	err = conn.QueryRowContext(ctx,
		"SELECT schema_version FROM flowkit_schema_version WHERE component = ?", component,
	).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		if cacheTableExists {
			return fmt.Errorf("mysql cache: schema version component %q is missing for existing table %q", component, c.table)
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
  key_digest    VARBINARY(64) NOT NULL PRIMARY KEY,
  step          MEDIUMTEXT NOT NULL,
  version       MEDIUMTEXT NOT NULL,
  input_digest  VARBINARY(64) NOT NULL,
  value_digest  VARBINARY(64) NOT NULL,
  value_json    MEDIUMTEXT NOT NULL,
  created_at    BIGINT NOT NULL
) ENGINE=InnoDB`, quoteMySQLIdentifier(c.table))); err != nil {
			return fmt.Errorf("create cache table: %w", err)
		}
		if _, err := conn.ExecContext(ctx,
			"INSERT INTO flowkit_schema_version(component, schema_version) VALUES(?, ?)",
			component, mysqlCacheSchemaV1,
		); err != nil {
			return fmt.Errorf("record cache schema version: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read cache schema version: %w", err)
	}
	if version != mysqlCacheSchemaV1 {
		return fmt.Errorf("mysql cache: unsupported schema version %d for %q (want %d)", version, component, mysqlCacheSchemaV1)
	}
	if !cacheTableExists {
		return fmt.Errorf("mysql cache: schema version %d is recorded but table %q is missing", version, c.table)
	}
	return nil
}

func mysqlTableExists(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table string) (bool, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&count)
	return count > 0, err
}

func mysqlCacheSchemaComponent(table string) string {
	return "execution.cache." + table
}

func mysqlCacheMigrationLock(database string) string {
	sum := sha256.Sum256([]byte(database))
	return "flowkit.cache." + hex.EncodeToString(sum[:])[:48]
}

func quoteMySQLIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

// Load validates the schema, full key, value digest, and result JSON exactly
// like FileCache.Load, then decodes the value into target. A missing entry
// returns (false, nil); a present-but-invalid entry returns ErrCorruptCache.
func (c *MySQLCache) Load(ctx context.Context, key Key, target any) (bool, error) {
	if c == nil || c.db == nil {
		return false, fmt.Errorf("mysql cache is nil")
	}
	if target == nil {
		return false, fmt.Errorf("cache load target is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	keyDigest, err := key.Digest()
	if err != nil {
		return false, err
	}
	var raw []byte
	err = c.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT value_json FROM %s WHERE key_digest = ?", quoteMySQLIdentifier(c.table)), keyDigest).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read mysql cache entry: %w", err)
	}
	// Mirror FileCache.Load's oversized-entry check: an entry written by an
	// instance with a larger limit (or inserted outside this cache) is corrupt
	// from this cache's perspective, not a silent oversized return.
	if int64(len(raw)) > c.maxEntryBytes {
		return false, fmt.Errorf("%w: entry exceeds maximum size", ErrCorruptCache)
	}
	envelope, err := jsonutil.DecodeStrict[cacheEnvelope](raw)
	if err != nil {
		return false, fmt.Errorf("%w: decode envelope: %v", ErrCorruptCache, err)
	}
	if envelope.SchemaVersion != cacheEntrySchema ||
		envelope.Key != key ||
		envelope.ValueDigest != digest.Bytes(envelope.Value) {
		return false, fmt.Errorf("%w: envelope validation failed", ErrCorruptCache)
	}
	if err := jsonutil.DecodeStrictInto(envelope.Value, target); err != nil {
		return false, fmt.Errorf("%w: decode value: %v", ErrCorruptCache, err)
	}
	return true, nil
}

// Store publishes a complete result envelope. The ON DUPLICATE KEY UPDATE is
// idempotent because cache entries are content-addressed: writing the same key
// twice writes the same value.
func (c *MySQLCache) Store(ctx context.Context, key Key, value any) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("mysql cache is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	keyDigest, err := key.Digest()
	if err != nil {
		return err
	}
	valueData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal cache value: %w", err)
	}
	envelope := cacheEnvelope{
		SchemaVersion: cacheEntrySchema,
		Key:           key,
		ValueDigest:   digest.Bytes(valueData),
		Value:         valueData,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal cache envelope: %w", err)
	}
	if int64(len(data)) > c.maxEntryBytes {
		return fmt.Errorf("cache entry is %d bytes, maximum is %d", len(data), c.maxEntryBytes)
	}
	_, err = c.db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s
  (key_digest, step, version, input_digest, value_digest, value_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  step         = VALUES(step),
  version      = VALUES(version),
  input_digest = VALUES(input_digest),
  value_digest = VALUES(value_digest),
  value_json   = VALUES(value_json),
  created_at   = VALUES(created_at)`,
		quoteMySQLIdentifier(c.table)),
		keyDigest, key.Step, key.Version, key.InputDigest, envelope.ValueDigest, data, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("publish mysql cache entry: %w", err)
	}
	return nil
}

// openMySQLPool opens a bounded database/sql connection pool for the mysql
// driver. The driver is registered by the blank import of
// github.com/go-sql-driver/mysql. Defaults match the design's pool sizing.
func openMySQLPool(dsn string, maxOpen, maxIdle int, maxLifetime, maxIdleTime time.Duration) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if maxOpen <= 0 {
		maxOpen = 20
	}
	if maxIdle <= 0 {
		maxIdle = 10
	}
	if maxLifetime <= 0 {
		maxLifetime = 5 * time.Minute
	}
	if maxIdleTime <= 0 {
		maxIdleTime = time.Minute
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(maxLifetime)
	db.SetConnMaxIdleTime(maxIdleTime)
	return db, nil
}
