package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	return fmt.Sprintf("cache_test_%s", sanitizeForTable(t.Name()))
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
	// Start from a clean table so re-running against the persistent shared DB
	// (table names are stable per test) does not collide with leftover rows.
	_, _ = cache.db.ExecContext(context.Background(), fmt.Sprintf("DELETE FROM %s", table))
	t.Cleanup(func() {
		_, _ = cache.db.ExecContext(context.Background(), fmt.Sprintf("DELETE FROM %s", table))
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
	// Concurrent Store of the same content-addressed key must be idempotent.
	for i := 0; i < 5; i++ {
		require.NoError(t, cache.Store(context.Background(), key, want))
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
	t.Cleanup(func() { _ = second.Close() })
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
		require.ErrorContains(t, err, "valid unquoted MySQL identifier")
	}
}

// TestNewMySQLCacheAcceptsValidTableName accepts the default and a couple of
// legal identifiers to guard against an over-strict regex.
func TestNewMySQLCacheAcceptsValidTableName(t *testing.T) {
	t.Parallel()
	for _, good := range []string{"cache_entries", "Cache1", "with_underscore", "dollar$"} {
		cache, err := NewMySQLCache(context.Background(), MySQLCacheOptions{
			DSN:       mysqlTestDSN(t),
			TableName: good,
		})
		require.NoError(t, err, "table %q should be accepted", good)
		t.Cleanup(func() { _ = cache.Close() })
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
	longStep := strings.Repeat("a", 200)
	longVersion := "v" + strings.Repeat("b", 200)
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

// digestOf builds a lowercase hex SHA-256 so the oversized-row test can
// construct a valid envelope without importing the internal digest package.
func digestOf(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
