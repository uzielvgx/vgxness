package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRetireSDDSkillPreservesModifiedBytes(t *testing.T) {
	files, err := bundledFiles("sdd-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	for _, modified := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact", true: "modified"}[modified], func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "skills")
			path := filepath.Join(root, "sdd-lifecycle", "SKILL.md")
			data := append([]byte(nil), files["SKILL.md"]...)
			if modified {
				data = append(data, '\n')
			}
			assertWrite(t, path, data)
			result, err := New().Install(context.Background(), Options{Dir: root})
			if modified {
				if err == nil {
					t.Fatal("modified retired skill accepted")
				}
				assertFileBytes(t, path, data)
				return
			}
			if err != nil || result.BackupPath == "" {
				t.Fatalf("retire: %+v %v", result, err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("retired skill remains: %v", err)
			}
			assertFileBytes(t, filepath.Join(result.BackupPath, "sdd-lifecycle", "SKILL.md"), data)
		})
	}
}
