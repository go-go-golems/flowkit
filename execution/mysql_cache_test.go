package execution

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mysqlTestDSN returns a DSN for the local docker-compose MySQL when one is
// configured. Tests skip (not fail) when no DSN is set, so `go test ./...`
// stays green in environments without MySQL (local/CI default). Set
// FLOWKIT_MYSQL_CACHE_DSN to run against a real MySQL.
func mysqlTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("FLOWKIT_MYSQL_CACHE_DSN")
	if dsn == "" {
		t.Skipf("FLOWKIT_MYSQL_CACHE_DSN not set; skipping MySQL cache integration test")
	}
	return dsn
}

// uniqueCacheTable returns a per-test table name so parallel tests do not
// collide on the shared cache_entries table.
func uniqueCacheTable(t *testing.T) string {
	t.Helper()
	name := sanitizeForTable(t.Name())
	sum := sha256.Sum256([]byte(t.Name()))
	if len(name) > 40 {
		name = name[:40]
	}
	return fmt.Sprintf("cache_test_%s_%s", name, hex.EncodeToString(sum[:])[:8])
}

func sanitizeForTable(name string) string {
	out := make([]byte, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, byte(r))
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

func newTestMySQLCache(t *testing.T) *MySQLCache {
	t.Helper()
	table := uniqueCacheTable(t)
	cache, err := NewMySQLCache(context.Background(), MySQLCacheOptions{
		DSN:       mysqlTestDSN(t),
		TableName: table,
	})
	require.NoError(t, err, "NewMySQLCache")
	t.Cleanup(func() {
		_, _ = cache.db.ExecContext(context.Background(), "DELETE FROM flowkit_schema_version WHERE component = ?", mysqlCacheSchemaComponent(table))
		_, _ = cache.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+quoteMySQLIdentifier(table))
		_ = cache.Close()
	})
	return cache
}

func TestMySQLCacheRoundTrip(t *testing.T) {
	t.Parallel()
	cache := newTestMySQLCache(t)

	key, err := NewKey("embed", "v1", map[string]string{"document": "doc-1", "model": "model-a"})
	require.NoError(t, err)
	want := cachedFixture{ID: "doc-1", Values: []float32{1, 2, 3}}
	require.NoError(t, cache.Store(context.Background(), key, want))

	var got cachedFixture
	found, err := cache.Load(context.Background(), key, &got)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)
}

func TestMySQLCacheLoadMissingReturnsFalseNil(t *testing.T) {
	t.Parallel()
	cache := newTestMySQLCache(t)
	key, err := NewKey("embed", "v1", "never-stored")
	require.NoError(t, err)
	var got cachedFixture
	found, err := cache.Load(context.Background(), key, &got)
	require.NoError(t, err)
	require.False(t, found)
}

func TestMySQLCacheFailsClosedOnCorruption(t *testing.T) {
	t.Parallel()
	cache := newTestMySQLCache(t)
	key, err := NewKey("embed", "v1", "doc-1")
	require.NoError(t, err)
	require.NoError(t, cache.Store(context.Background(), key, cachedFixture{ID: "doc-1"}))

	keyDigest, err := key.Digest()
	require.NoError(t, err)
	// Corrupt the stored envelope: wrong schema_version breaks validation.
	_, err = cache.db.ExecContext(context.Background(),
		fmt.Sprintf("UPDATE %s SET value_json = ? WHERE key_digest = ?", cache.table),
		[]byte(`{"schema_version":"wrong"}`), keyDigest)
	require.NoError(t, err)

	var result cachedFixture
	_, err = cache.Load(context.Background(), key, &result)
	require.True(t, errors.Is(err, ErrCorruptCache), "want ErrCorruptCache, got %v", err)
}

func TestMySQLCacheStoreIsIdempotent(t *testing.T) {
	t.Parallel()
	cache := newTestMySQLCache(t)
	key, err := NewKey("embed", "v1", "doc-1")
	require.NoError(t, err)
	want := cachedFixture{ID: "doc-1", Values: []float32{1, 2, 3}}
	// Concurrent Store of the same content-addressed key must publish one row.
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- cache.Store(context.Background(), key, want)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var got cachedFixture
	found, err := cache.Load(context.Background(), key, &got)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)

	// Only one row exists for the key.
	var n int
	require.NoError(t, cache.db.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE key_digest = ?", cache.table), keyDigestOrFatal(t, key)).Scan(&n))
	require.Equal(t, 1, n, "content-addressed key must produce exactly one row")
}

func TestMySQLCacheOverwriteUpdatesValue(t *testing.T) {
	t.Parallel()
	cache := newTestMySQLCache(t)
	key, err := NewKey("embed", "v1", "doc-1")
	require.NoError(t, err)
	require.NoError(t, cache.Store(context.Background(), key, cachedFixture{ID: "doc-1", Values: []float32{1}}))
	require.NoError(t, cache.Store(context.Background(), key, cachedFixture{ID: "doc-1", Values: []float32{2, 2}}))
	var got cachedFixture
	found, err := cache.Load(context.Background(), key, &got)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, cachedFixture{ID: "doc-1", Values: []float32{2, 2}}, got)
}

func TestMySQLCacheSurvivesRestart(t *testing.T) {
	// The durability test that beats sync-on-end: entries written by one cache
	// handle are hits on a fresh handle backed by the same table.
	t.Parallel()
	dsn := mysqlTestDSN(t)
	table := uniqueCacheTable(t)

	first, err := NewMySQLCache(context.Background(), MySQLCacheOptions{DSN: dsn, TableName: table})
	require.NoError(t, err)
	key, err := NewKey("embed", "v1", "durable-doc")
	require.NoError(t, err)
	want := cachedFixture{ID: "durable-doc", Values: []float32{9, 9, 9}}
	require.NoError(t, first.Store(context.Background(), key, want))
	require.NoError(t, first.Close())

	second, err := NewMySQLCache(context.Background(), MySQLCacheOptions{DSN: dsn, TableName: table})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = second.db.ExecContext(context.Background(), "DELETE FROM flowkit_schema_version WHERE component = ?", mysqlCacheSchemaComponent(table))
		_, _ = second.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+quoteMySQLIdentifier(table))
		_ = second.Close()
	})
	var got cachedFixture
	found, err := second.Load(context.Background(), key, &got)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)
}

func TestMySQLCacheCompatibleWithFileCacheEnvelope(t *testing.T) {
	// The value_json column is byte-identical to a FileCache file, so a value
	// written by FileCache and copied into MySQL reads back through MySQLCache.
	t.Parallel()
	dsn := mysqlTestDSN(t)
	table := uniqueCacheTable(t)

	dir := t.TempDir()
	fileCache, err := NewFileCache(FileCacheOptions{Directory: dir})
	require.NoError(t, err)
	key, err := NewKey("embed", "v1", "shared-doc")
	require.NoError(t, err)
	want := cachedFixture{ID: "shared-doc", Values: []float32{4, 5, 6}}
	require.NoError(t, fileCache.Store(context.Background(), key, want))
	path, err := fileCache.path(key)
	require.NoError(t, err)
	envelopeBytes, err := os.ReadFile(path)
	require.NoError(t, err)

	mysqlCache, err := NewMySQLCache(context.Background(), MySQLCacheOptions{DSN: dsn, TableName: table})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mysqlCache.Close() })
	// Start clean so a re-run against the persistent shared DB does not collide
	// with a leftover row (this test does a raw INSERT without ON DUPLICATE KEY).
	_, _ = mysqlCache.db.ExecContext(context.Background(), fmt.Sprintf("DELETE FROM %s", table))
	keyDigest, err := key.Digest()
	require.NoError(t, err)
	_, err = mysqlCache.db.ExecContext(context.Background(),
		fmt.Sprintf("INSERT INTO %s (key_digest, step, version, input_digest, value_digest, value_json, created_at) VALUES (?,?,?,?,?,?,?)", table),
		keyDigest, key.Step, key.Version, key.InputDigest, "placeholder", envelopeBytes, time.Now().UnixMilli())
	require.NoError(t, err)

	var got cachedFixture
	found, err := mysqlCache.Load(context.Background(), key, &got)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)
}

// keyDigestOrFatal is a small helper to keep the idempotent-row test readable.
func keyDigestOrFatal(t *testing.T, key Key) string {
	t.Helper()
	d, err := key.Digest()
	require.NoError(t, err)
	return d
}

// Ensure the test helper imports stay referenced.
var _ = filepath.Separator

// TestNewMySQLCacheRejectsInvalidTableName verifies the table identifier is
// validated before any SQL is built, so a configured name like "flow-cache" or
// an injection attempt fails fast instead of producing a syntax error or a
// multi-statement hazard.
func TestNewMySQLCacheRejectsInvalidTableName(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"flow-cache", "foo; DROP TABLE x;--", "has space", "9starts-digit"} {
		_, err := NewMySQLCache(context.Background(), MySQLCacheOptions{
			DSN:       mysqlTestDSN(t),
			TableName: bad,
		})
		require.Error(t, err, "table %q should be rejected", bad)
		require.ErrorContains(t, err, "valid MySQL identifier")
	}
}

// TestNewMySQLCacheAcceptsValidTableName accepts the default and a couple of
// legal identifiers to guard against an over-strict regex.
func TestNewMySQLCacheAcceptsValidTableName(t *testing.T) {
	t.Parallel()
	for _, good := range []string{"cache_entries", "Cache1", "with_underscore", "dollar$", "select"} {
		cache, err := NewMySQLCache(context.Background(), MySQLCacheOptions{
			DSN:       mysqlTestDSN(t),
			TableName: good,
		})
		require.NoError(t, err, "table %q should be accepted", good)
		t.Cleanup(func() {
			_, _ = cache.db.ExecContext(context.Background(), "DELETE FROM flowkit_schema_version WHERE component = ?", mysqlCacheSchemaComponent(good))
			_, _ = cache.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+quoteMySQLIdentifier(good))
			_ = cache.Close()
		})
	}
}

// TestNewMySQLCacheCapsMaxEntryBytes verifies that a MaxEntryBytes larger than
// MEDIUMTEXT capacity is rejected, and the default equals that capacity (not
// 16<<20, which is one byte over MEDIUMTEXT and would admit an unstorable size).
func TestNewMySQLCacheCapsMaxEntryBytes(t *testing.T) {
	t.Parallel()
	_, err := NewMySQLCache(context.Background(), MySQLCacheOptions{
		DSN:           mysqlTestDSN(t),
		TableName:     uniqueCacheTable(t),
		MaxEntryBytes: mysqlMediumTextMax + 1,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "exceeds MEDIUMTEXT capacity")

	cache, err := NewMySQLCache(context.Background(), MySQLCacheOptions{
		DSN:       mysqlTestDSN(t),
		TableName: uniqueCacheTable(t) + "_default",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cache.Close() })
	require.Equal(t, int64(mysqlMediumTextMax), cache.maxEntryBytes)
}

// TestMySQLCacheLoadRejectsOversizedRow mirrors FileCache.Load's behavior: an
// existing row larger than the configured MaxEntryBytes is ErrCorruptCache,
// not a silent oversized return. The row is inserted directly (bypassing the
// Store size check) to simulate a larger-limit instance or an out-of-band insert.
func TestMySQLCacheLoadRejectsOversizedRow(t *testing.T) {
	t.Parallel()
	dsn := mysqlTestDSN(t)
	table := uniqueCacheTable(t)
	cache, err := NewMySQLCache(context.Background(), MySQLCacheOptions{
		DSN:           dsn,
		TableName:     table,
		MaxEntryBytes: 64,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = cache.db.ExecContext(context.Background(), fmt.Sprintf("DELETE FROM %s", table))
		_ = cache.Close()
	})
	// Start clean so a re-run against the persistent shared DB does not collide
	// with a leftover row (this test does a raw INSERT without ON DUPLICATE KEY).
	_, _ = cache.db.ExecContext(context.Background(), fmt.Sprintf("DELETE FROM %s", table))

	key, err := NewKey("embed", "v1", "big-doc")
	require.NoError(t, err)
	// Build a valid envelope (so it would pass validation) but larger than 64 bytes.
	valueData, err := json.Marshal(cachedFixture{ID: "big-doc", Values: []float32{1, 2, 3, 4, 5, 6, 7, 8}})
	require.NoError(t, err)
	envelope := cacheEnvelope{
		SchemaVersion: cacheEntrySchema,
		Key:           key,
		ValueDigest:   digestOf(valueData),
		Value:         valueData,
	}
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.Greater(t, len(data), 64, "fixture envelope must exceed the small limit")
	keyDigest, err := key.Digest()
	require.NoError(t, err)
	_, err = cache.db.ExecContext(context.Background(),
		fmt.Sprintf("INSERT INTO %s (key_digest, step, version, input_digest, value_digest, value_json, created_at) VALUES (?,?,?,?,?,?,?)", table),
		keyDigest, key.Step, key.Version, key.InputDigest, envelope.ValueDigest, data, time.Now().UnixMilli())
	require.NoError(t, err)

	var got cachedFixture
	_, err = cache.Load(context.Background(), key, &got)
	require.ErrorIs(t, err, ErrCorruptCache)
}

// TestMySQLCacheSupportsLongStepAndVersion verifies that a valid Key whose Step
// or Version exceeds 64 chars (allowed by NewKey/validate and by FileCache) is
// stored and loaded through MySQLCache without a data-too-long error.
func TestMySQLCacheSupportsLongStepAndVersion(t *testing.T) {
	t.Parallel()
	cache := newTestMySQLCache(t)
	longStep := strings.Repeat("a", 1024)
	longVersion := "v" + strings.Repeat("b", 1024)
	key, err := NewKey(longStep, longVersion, "long-key-doc")
	require.NoError(t, err)
	want := cachedFixture{ID: "long-key-doc", Values: []float32{9}}
	require.NoError(t, cache.Store(context.Background(), key, want))
	var got cachedFixture
	found, err := cache.Load(context.Background(), key, &got)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, got)
}

func TestMySQLCacheSchemaUsesExactIdentityAndUnboundedMetadata(t *testing.T) {
	t.Parallel()
	cache := newTestMySQLCache(t)

	want := map[string]string{
		"key_digest":   "varbinary",
		"step":         "mediumtext",
		"version":      "mediumtext",
		"input_digest": "varbinary",
		"value_digest": "varbinary",
	}
	rows, err := cache.db.QueryContext(context.Background(), `SELECT column_name, data_type
FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ?`, cache.table)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	for rows.Next() {
		var column, dataType string
		require.NoError(t, rows.Scan(&column, &dataType))
		if expected, ok := want[column]; ok {
			require.Equal(t, expected, dataType, "column %s", column)
			delete(want, column)
		}
	}
	require.NoError(t, rows.Err())
	require.Empty(t, want, "expected identity and metadata columns were not inspected")
}

func TestMySQLCacheMigrationRecoversFromInitializationMarker(t *testing.T) {
	dsn := mysqlTestDSN(t)
	table := uniqueCacheTable(t)
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	component := mysqlCacheSchemaComponent(table)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM flowkit_schema_version WHERE component = ?", component)
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+quoteMySQLIdentifier(table))
		_ = db.Close()
	})
	_, err = db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS flowkit_schema_version (
  component VARBINARY(128) NOT NULL PRIMARY KEY,
  schema_version BIGINT NOT NULL
) ENGINE=InnoDB`)
	require.NoError(t, err)
	_, err = db.ExecContext(context.Background(),
		"INSERT INTO flowkit_schema_version(component, schema_version) VALUES(?, 0)", component)
	require.NoError(t, err)
	// Simulate interruption after CREATE TABLE's implicit commit but before
	// the component version can be finalized.
	require.NoError(t, createMySQLCacheSchemaV1(context.Background(), db, table))

	cache, err := NewMySQLCache(context.Background(), MySQLCacheOptions{DSN: dsn, TableName: table})
	require.NoError(t, err)
	require.NoError(t, cache.Close())
	var version int64
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT schema_version FROM flowkit_schema_version WHERE component = ?", component).Scan(&version))
	require.Equal(t, mysqlCacheSchemaV1, version)
}

func TestMySQLCacheTableInspectionUsesCaseSensitiveIdentity(t *testing.T) {
	t.Parallel()
	dsn := mysqlTestDSN(t)
	upperTable := "CacheCaseUpper"
	lowerTable := "cachecaseupper"
	upper, err := NewMySQLCache(context.Background(), MySQLCacheOptions{DSN: dsn, TableName: upperTable})
	require.NoError(t, err)
	lower, err := NewMySQLCache(context.Background(), MySQLCacheOptions{DSN: dsn, TableName: lowerTable})
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, table := range []string{upperTable, lowerTable} {
			_, _ = upper.db.ExecContext(context.Background(), "DELETE FROM flowkit_schema_version WHERE component = ?", mysqlCacheSchemaComponent(table))
			_, _ = upper.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+quoteMySQLIdentifier(table))
		}
		_ = lower.Close()
		_ = upper.Close()
	})
	var count int
	require.NoError(t, upper.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema = DATABASE() AND BINARY table_name IN (BINARY ?, BINARY ?)`, upperTable, lowerTable).Scan(&count))
	require.Equal(t, 2, count)
}

func TestNewMySQLCacheRejectsUnversionedPrototype(t *testing.T) {
	t.Parallel()
	dsn := mysqlTestDSN(t)
	table := uniqueCacheTable(t)
	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+quoteMySQLIdentifier(table))
		_ = db.Close()
	})
	_, err = db.ExecContext(context.Background(), "CREATE TABLE "+quoteMySQLIdentifier(table)+" (key_digest VARBINARY(64) PRIMARY KEY) ENGINE=InnoDB")
	require.NoError(t, err)

	cache, err := NewMySQLCache(context.Background(), MySQLCacheOptions{DSN: dsn, TableName: table})
	if cache != nil {
		_ = cache.Close()
	}
	require.Error(t, err)
	require.True(t,
		strings.Contains(err.Error(), "unversioned prototype") || strings.Contains(err.Error(), "schema version component"),
		"unexpected schema-gate error: %v", err,
	)
}

// digestOf builds a lowercase hex SHA-256 so the oversized-row test can
// construct a valid envelope without importing the internal digest package.
func digestOf(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
