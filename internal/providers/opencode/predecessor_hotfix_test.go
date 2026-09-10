package opencode

import (
	"bytes"

	"testing"
)

func TestSameVersionManagedArtifactRequiresExactRecognizer(t *testing.T) {
	current := []byte("<!-- managed-by: vgxness; artifact: opencode-agent/general; version: 10 -->\ncurrent")
	predecessor := []byte("<!-- managed-by: vgxness; artifact: opencode-agent/general; version: 10 -->\nalpha.2")
	recognize := func(candidate []byte) bool { return bytes.Equal(candidate, predecessor) }
	if !isManagedPredecessor(predecessor, current, nil, recognize) {
		t.Fatal("exact same-version predecessor was not recognized")
	}
	if isManagedPredecessor(append(predecessor, '\n'), current, nil, recognize) {
		t.Fatal("one-byte same-version drift was recognized")
	}
}
