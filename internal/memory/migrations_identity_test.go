package memory

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/vgxness/vgxness/internal/testutil"
)

func TestSchemaV10CumulativeIdentity(t *testing.T) {
	const schemaV10CumulativeHash = "541e918b41360b307e5473c37b6b785c63d09aef21c325674c93440378e84a3b"
	// Raw direct schemaV1...schemaV10 concatenation: no separator; excludes schemaV11, seeds, PRAGMA, and test SQL.
	cumulative := schemaV1 + schemaV2 + schemaV3 + schemaV4 + schemaV5 + schemaV6 + schemaV7 + schemaV8 + schemaV9 + schemaV10
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(cumulative)))
	testutil.Require(t, got == schemaV10CumulativeHash, "historical migration bytes are immutable; changing the V1-V10 cumulative schema requires an explicit compatibility decision: got %s want %s", got, schemaV10CumulativeHash)
}
