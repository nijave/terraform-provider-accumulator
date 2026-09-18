// SPDX-License-Identifier: GPL-3.0-or-later

package accumulate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// HashID returns the lowercase hex SHA-256 of the canonical JSON encoding of
// values. A nil slice is normalized to an empty one first, so an empty inputs
// list hashes "[]" rather than JSON null.
//
// JSON encoding makes the identifier deterministic: recreating a resource with
// the same initial inputs yields the same id. It is informational only, since
// Terraform keys state by resource address.
func HashID(values []string) string {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		// json.Marshal of a []string has no error path. Keeping the branch means
		// a future change to the input type cannot silently hash empty bytes.
		panic(fmt.Sprintf("accumulate: hashing %T: %v", values, err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
