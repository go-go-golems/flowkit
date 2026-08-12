package execution

import (
	"context"
	"path/filepath"
	"testing"
)

// This fixture was generated with ragkit's pre-extraction FileCache contract.
// Its literal bytes protect the schema, JSON tags, digest, and path layout from
// accidental migration drift.
func TestFileCacheLoadsPreExtractionFixture(t *testing.T) {
	key, err := NewKey("compat-step", "v1", struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}{ID: "item-1", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "de7120bfe3cbfd54a6f5559293e5cf14fd4f8c55a87628c75a6be447ea59f17d"
	gotDigest, err := key.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("key digest = %q, want pre-extraction digest %q", gotDigest, wantDigest)
	}

	cache, err := NewFileCache(FileCacheOptions{Directory: filepath.Join("testdata", "pre-extraction-cache")})
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Answer string `json:"answer"`
	}
	found, err := cache.Load(context.Background(), key, &value)
	if err != nil {
		t.Fatal(err)
	}
	if !found || value.Answer != "cached" {
		t.Fatalf("fixture load = found %v value %#v", found, value)
	}
}
