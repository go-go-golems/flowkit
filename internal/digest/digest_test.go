package digest

import "testing"

func TestBytes(t *testing.T) {
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := Bytes([]byte("abc")); got != want {
		t.Fatalf("Bytes(abc) = %q, want %q", got, want)
	}
}
