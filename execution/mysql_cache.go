package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-go-golems/flowkit/internal/digest"
	"github.com/go-go-golems/flowkit/internal/jsonutil"
	_ "github.com/go-sql-driver/mysql"
)

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
	maxEntryBytes := opts.MaxEntryBytes
	if maxEntryBytes == 0 {
		maxEntryBytes = 16 << 20
	}
	if maxEntryBytes < 1 {
		return nil, fmt.Errorf("mysql cache maximum entry bytes must be positive")
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
	// value_json holds the full envelope FileCache writes to disk; MEDIUMTEXT
	// matches the 16 MiB default maximum entry size. key_digest is Key.Digest()
	// (64-char hex SHA-256) and is the content-addressed primary key, so the
	// ON DUPLICATE KEY UPDATE in Store is an idempotent no-op for repeat writes.
	const stmt = `
CREATE TABLE IF NOT EXISTS %s (
  key_digest    CHAR(64) NOT NULL PRIMARY KEY,
  step          VARCHAR(64) NOT NULL,
  version       VARCHAR(64) NOT NULL,
  input_digest  CHAR(64) NOT NULL,
  value_digest  CHAR(64) NOT NULL,
  value_json    MEDIUMTEXT NOT NULL,
  created_at    BIGINT NOT NULL,
  KEY idx_step_version (step, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`
	if _, err := c.db.ExecContext(ctx, fmt.Sprintf(stmt, c.table)); err != nil {
		return err
	}
	return nil
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
		fmt.Sprintf("SELECT value_json FROM %s WHERE key_digest = ?", c.table), keyDigest).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read mysql cache entry: %w", err)
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
		c.table),
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
