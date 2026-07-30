package launcher

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzDecodeManifest(f *testing.F) {
	executable := filepath.Join(string(filepath.Separator), "tmp", binaryName())
	digest := strings.Repeat("a", 64)
	manifest := Manifest{
		SchemaVersion: SchemaVersion, ManagedBy: ManagedBy,
		LauncherPath: executable, LauncherSHA256: strings.Repeat("b", 64), DataDir: filepath.Join(string(filepath.Separator), "tmp", "data"),
		ActivePath: VersionPath(filepath.Join(string(filepath.Separator), "tmp", "data"), digest), ActiveSHA256: digest,
		PreviousSHA256: strings.Repeat("c", 64), UpdatedAt: "2026-07-30T00:00:00Z",
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte(`{"unknown":true}`))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, input []byte) {
		manifest, err := decodeManifest(input)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("unexpected decode error: %v", err)
			}
			return
		}
		if err := Validate(manifest, manifest.LauncherPath); err != nil {
			t.Fatalf("accepted invalid manifest: %+v: %v", manifest, err)
		}
	})
}
