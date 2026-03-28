package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashInstanceID produces a compact, dot-free instance segment for a 6-part
// entity ID. It hashes the input parts with SHA-256 and returns the first
// 16 hex characters (8 bytes).
//
// Convention: entity IDs are 6 dot-separated parts:
//
//	org.platform.domain.system.type.instance
//
// Parts 1-5 are fixed constants. Part 6 (instance) MUST NOT contain dots
// and should be compact. This function guarantees both properties.
//
// Parts are joined with a null separator to prevent collisions between
// ("a-b", "c") and ("a", "b-c").
//
// Collision probability is negligible for expected volumes (~2^64 birthday bound).
func HashInstanceID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:8])
}
