//go:build e2e

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerDeployPackageContract(t *testing.T) {
	repository := repositoryRoot(t)
	assets := map[string][]string{
		"deploy/docker/Dockerfile":              {"VGXNESS_GO_BUILDER_IMAGE", "VGXNESS_RUNTIME_IMAGE", "CGO_ENABLED=0", "TARGETOS", "TARGETARCH", "-trimpath", "-buildvcs=false", "-mod=readonly", "USER 65532:65532"},
		"deploy/docker/Dockerfile.dockerignore": {"**", "!go.mod", "!go.sum", "!cmd/vgxness-syncd/**", "!internal/**"},
		"deploy/docker/compose.yaml":            {"VGXNESS_GO_BUILDER_IMAGE", "VGXNESS_RUNTIME_IMAGE", "VGXNESS_SYNCD_ENV", "VGXNESS_POSTGRES_ADMIN_PASSWORD", "VGXNESS_SYNCD_PASSWORD", "VGXNESS_BACKUP_PASSWORD", "user: \"65532:65532\"", "backup:", "networks: [private]", "nginx-proxy-manager_default", "internal: true", "read_only: true", "0.0.0.0:8787", "--container-network"},
		"deploy/docker/backup.sh":               {"mkdir \"$lock\"", "trap 'cleanup; exit 130'", "[ ! -e \"$temporary\" ]", "pg_dump --format=custom", "pg_restore --list", "sha256sum", "sync -f", "mv -T --"},
		"deploy/docker/postgres-init.sh":        {"admin_password", "NOSUPERUSER", "vgxness_syncd_backup", "ALL SEQUENCES", "ALTER DEFAULT PRIVILEGES", "A-Za-z0-9._~-"},
		"deploy/docker/README.md":               {"COMPOSE_PROJECT_NAME", "POSTGRES_IMAGE", "VGXNESS_GO_BUILDER_IMAGE", "VGXNESS_RUNTIME_IMAGE", "VGXNESS_SYNCD_IMAGE", "docker image inspect \"$VGXNESS_SYNCD_IMAGE\"", "docker volume inspect vgxness_sync_postgres-data vgxness_sync_backup-data", "exec postgres psql --username postgres --dbname vgxness_sync", "\\password vgxness_syncd", "VGXNESS_SYNCD_ENV", "65532", "CRLF", "nginx-proxy-manager_default", "vgxness-syncd:8787", "401", "forward-only", "Rollback", "Backup", "Restore", "runtime is unverified"},
	}
	for relative, required := range assets {
		data, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		for _, want := range required {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s is missing %q", relative, want)
			}
		}
		for _, forbidden := range forbiddenDockerDeployText(relative) {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("%s contains forbidden %q", relative, forbidden)
			}
		}
	}
	compose, err := os.ReadFile(filepath.Join(repository, "deploy", "docker", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	postgres := composeServiceBlock(t, string(compose), "postgres")
	if strings.Contains(postgres, "cap_drop:") {
		t.Error("postgres cannot drop all capabilities before first-volume initialization is tested")
	}
	backup := composeServiceBlock(t, string(compose), "backup")
	for _, want := range []string{"profiles: [maintenance]", "networks: [private]", "backup-data:/backups", "backup_password", "read_only: true", "cap_drop: [ALL]"} {
		if !strings.Contains(backup, want) {
			t.Errorf("backup service is missing %q", want)
		}
	}
	init, err := os.ReadFile(filepath.Join(repository, "deploy", "docker", "postgres-init.sh"))
	if err != nil || !strings.Contains(string(init), "admin_password") || !strings.Contains(string(init), "DEFAULT PRIVILEGES FOR ROLE vgxness_syncd IN SCHEMA public GRANT SELECT ON SEQUENCES") || !strings.Contains(string(init), "DEFAULT PRIVILEGES FOR ROLE vgxness_syncd IN SCHEMA public GRANT SELECT ON TABLES") {
		t.Error("postgres init privilege or admin validation contract is incomplete")
	}
	backupScript, err := os.ReadFile(filepath.Join(repository, "deploy", "docker", "backup.sh"))
	if err != nil || strings.Index(string(backupScript), "mkdir \"$lock\"") > strings.Index(string(backupScript), "[ ! -e \"$temporary\"") {
		t.Error("backup lock must be acquired before checking the fixed temporary path")
	}
	makefile, err := os.ReadFile(filepath.Join(repository, "Makefile"))
	if err != nil || !strings.Contains(string(makefile), "TestDockerDeployPackageContract") {
		t.Error("Makefile verify target does not select the Docker deployment contract")
	}
}

func TestPostgresInitPasswordReaderIsNounsetSafe(t *testing.T) {
	repository := repositoryRoot(t)
	init, err := os.ReadFile(filepath.Join(repository, "deploy", "docker", "postgres-init.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(init), "IFS= read -r value") {
		t.Fatal("postgres password reader uses the BusyBox-unsafe IFS= read form")
	}
	start := strings.Index(string(init), "password()")
	end := strings.Index(string(init), "\nvalid()")
	if start < 0 || end < 0 {
		t.Fatal("postgres password reader is missing")
	}
	reader := string(init)[start:end]
	for name, contents := range map[string]string{
		"newline":          "correct-horse-battery-staple\n",
		"no-final-newline": "correct-horse-battery-staple",
	} {
		t.Run(name, func(t *testing.T) {
			secret := filepath.Join(t.TempDir(), "password")
			if err := os.WriteFile(secret, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("sh", "-eu", "-c", reader+"\npassword \"$1\"", "postgres-init-reader", secret)
			output, err := command.Output()
			if err != nil {
				t.Fatalf("password reader failed under sh -eu: %v", err)
			}
			if got, want := string(output), strings.TrimSuffix(contents, "\n"); got != want {
				t.Errorf("password reader output = %q, want %q", got, want)
			}
		})
	}
}

func composeServiceBlock(t *testing.T, compose, name string) string {
	t.Helper()
	start := strings.Index(compose, "  "+name+":\n")
	if start < 0 {
		t.Fatalf("compose service %q is missing", name)
	}
	block := compose[start:]
	if end := strings.Index(block, "\n\n  "); end >= 0 {
		return block[:end]
	}
	return block
}

func forbiddenDockerDeployText(relative string) []string {
	switch relative {
	case "deploy/docker/compose.yaml":
		return []string{"ports:", "VGXNESS_SYNC_POSTGRES_DSN:", "./syncd.env", "./syncd-dsn", "./postgres-password"}
	case "deploy/docker/Dockerfile":
		return []string{"USER root"}
	case "deploy/docker/backup.sh":
		return []string{"rm -rf", "find ", "current.pgd.sha256", "$hash"}
	case "deploy/docker/Dockerfile.dockerignore":
		return []string{"!deploy/", "!**/*.env", "!**/*password*", "!**/*secret*"}
	default:
		return nil
	}
}
