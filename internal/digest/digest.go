// Package digest provides the stable SHA-256 operation used by Flowkit cache identities.
package digest

import (
	"crypto/sha256"
	"encoding/hex"
)

// Bytes returns the lowercase hexadecimal SHA-256 digest of data.
func Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
