//go:build e2e

package e2e_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUbuntuDeployPackageContract(t *testing.T) {
	repository := repositoryRoot(t)
	assets := map[string][]string{
		"deploy/ubuntu/vgxness-syncd.service": {
			"User=vgxness-syncd", "EnvironmentFile=/etc/vgxness-syncd/syncd.env",
			"ExecStart=/opt/vgxness-syncd/current/vgxness-syncd serve --listen 127.0.0.1:8787",
			"IPAddressDeny=any", "IPAddressAllow=localhost", "NoNewPrivileges=true",
		},
		"deploy/ubuntu/vgxness-syncd.env.example": {
			"VGXNESS_SYNC_POSTGRES_DSN=postgres://", "VGXNESS_SYNC_OWNER_ID=",
		},
		"deploy/ubuntu/postgresql-bootstrap.sql.example": {
			"\\set ON_ERROR_STOP on", "CREATE ROLE vgxness_syncd LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION",
			"CREATE ROLE vgxness_syncd_backup LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION",
			"CREATE DATABASE vgxness_sync OWNER vgxness_syncd", "\\password vgxness_syncd", "\\password vgxness_syncd_backup",
			"GRANT SELECT ON ALL TABLES IN SCHEMA public TO vgxness_syncd_backup",
		},
		"deploy/ubuntu/pg_service.conf.example": {
			"[vgxness_sync_backup]", "host=127.0.0.1", "dbname=vgxness_sync", "user=vgxness_syncd_backup",
		},
		"deploy/ubuntu/pgpass.example": {
			"127.0.0.1:5432:vgxness_sync:vgxness_syncd_backup:REPLACE_WITH_GENERATED_PASSWORD",
		},
		"deploy/ubuntu/vgxness-syncd-backup.service": {
			"User=vgxness-syncd-backup", "Group=vgxness-syncd-backup", "StateDirectory=vgxness-syncd-backup",
			"StateDirectoryMode=0700", "PGSERVICE=vgxness_sync_backup", "PGSERVICEFILE=/etc/vgxness-syncd-backup/pg_service.conf",
			"PGPASSFILE=/etc/vgxness-syncd-backup/pgpass", "IPAddressDeny=any", "IPAddressAllow=localhost",
			"pg_dump --format=custom --no-owner --no-privileges --file=.current.pgd.tmp",
			"pg_restore --list .current.pgd.tmp", "sync -f .current.pgd.tmp",
			"sha256sum .current.pgd.tmp", "mv -T -- .current.pgd.tmp current.pgd",
			".current.pgd.sha256.tmp", "current.pgd.sha256", "chmod 0600 .current.pgd.sha256.tmp",
			"flock --nonblock /var/lib/vgxness-syncd-backup/backup.lock",
			"ExecStartPre=-/usr/bin/rm -f -- /var/lib/vgxness-syncd-backup/.current.pgd.tmp /var/lib/vgxness-syncd-backup/.current.pgd.sha256.tmp",
			"cd /var/lib/vgxness-syncd-backup", "sha256sum .current.pgd.tmp", "current.pgd",
			"sha256sum .current.pgd.tmp > .current.pgd.sha256.tmp", "sed -i \"s|\\\\.current\\\\.pgd\\\\.tmp|current.pgd|\" .current.pgd.sha256.tmp",
			"sync -f current.pgd",
		},
		"deploy/ubuntu/vgxness-syncd-backup.timer": {
			"OnCalendar=daily", "Persistent=true",
		},
		"deploy/ubuntu/README.md": {
			"Ubuntu 24.04", "authorized root operator", "DBA/maintenance approval", "0600", "systemd-analyze verify",
			"systemctl daemon-reload", "systemctl restart vgxness-syncd",
			"is-active", "ss -ltn", "401", "Preflight", "Abort", "Blast radius", "Restore", "Rollback", "Escalation",
			"[A-Za-z0-9._~-]", ">=32", "/opt/vgxness-syncd/current/vgxness-syncd", "vgxness_syncd_backup",
			"--single-transaction", "--role=vgxness_syncd", "device issue", "device revoke", "/v1/sync/capabilities",
			"getent passwd vgxness-syncd", "getent group vgxness-syncd-backup", "useradd --system", "/usr/sbin/nologin",
			"root:vgxness-syncd-backup", "0750", "root:vgxness-syncd-backup` and mode `0640", "INVESTIGATION_VERIFIED.pgd", "STAGED_UNIT_DIRECTORY",
			"files do not execute actions by themselves", "authorized operator executes the reviewed steps",
			"systemctl enable --now vgxness-syncd", "APPROVED_RELEASE_SHA256", "readlink -f /opt/vgxness-syncd/current",
			"sha256sum /opt/vgxness-syncd/current/vgxness-syncd", "migration ledger", "approved database recovery/forward-fix",
			"systemctl disable --now vgxness-syncd-backup.timer", "flock",
			"env -i PATH=/usr/bin:/bin",
			"system_identifier", "--host=/var/run/postgresql", "--port=5432", "--username=postgres", "sha256sum --check",
			"EXPECTED_SYSTEM_IDENTIFIER", "test -n \"$EXPECTED_SYSTEM_IDENTIFIER\"", "cd /var/lib/vgxness-syncd-backup", "sha256sum --check current.pgd.sha256",
			"exec 9>/var/lib/vgxness-syncd-backup/backup.lock", "flock -n 9", "Pre-publication failure preserves the prior verified generation",
			"set -eu", "umask 077", "/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin",
			"/usr/bin/pg_dump", "--file=- > INVESTIGATION_VERIFIED.pgd", "/usr/bin/pg_restore", "< current.pgd",
			"systemctl enable --now vgxness-syncd-backup.timer", "timer remains stopped",
			"http://127.0.0.1:8787/v1/sync/capabilities", "%{http_code}", "= 401", "does not configure public ingress",
			"Do not use the native Ubuntu package as a containerized Nginx Proxy Manager upstream", "repository Docker deployment", "syncd joins the verified external Nginx Proxy Manager network", "Mere Nginx Proxy Manager network attachment cannot reach host loopback", "external ingress must remain stopped",
			"## Legacy Caddy retirement (upgrade only)", "inspection proves a prior VGXNESS Caddy route is installed", "never an initial-install action", "exact recorded ownership/revision", "Never delete a shared Caddyfile", "unrelated sites", "no longer routes to 127.0.0.1:8787", "inspection/validation/controlled-reload gates",
		},
	}
	assertSectionOrdered(t, filepath.Join(repository, "deploy", "ubuntu", "vgxness-syncd-backup.service"), "ExecStart=/usr/bin/flock", "\nNoNewPrivileges=",
		"sha256sum .current.pgd.tmp",
		".current.pgd.sha256.tmp",
		"mv -T -- .current.pgd.tmp current.pgd",
		"mv -T -- .current.pgd.sha256.tmp current.pgd.sha256")
	assertMinOccurrences(t, filepath.Join(repository, "deploy", "ubuntu", "README.md"),
		"test \"$CURRENT_SYSTEM_IDENTIFIER\" = \"$EXPECTED_SYSTEM_IDENTIFIER\"", 3)
	assertSectionOrdered(t, filepath.Join(repository, "deploy", "ubuntu", "README.md"), "```sh\n# Run this block as root", "\n```",
		"exec 9>/var/lib/vgxness-syncd-backup/backup.lock",
		"flock -n 9",
		"sha256sum --check current.pgd.sha256",
		"dropdb --host=/var/run/postgresql")
	assertSectionOrdered(t, filepath.Join(repository, "deploy", "ubuntu", "README.md"), "```sh\n# Run this block as root", "\n```",
		"set -eu", "umask 077", "flock -n 9", "dropdb --host=/var/run/postgresql", "systemctl is-active vgxness-syncd",
		"http://127.0.0.1:8787/v1/sync/capabilities", "systemctl enable --now vgxness-syncd-backup.timer")
	assertSectionOrdered(t, filepath.Join(repository, "deploy", "ubuntu", "README.md"), "## Immutable update and rollback", "\n## Backup and restore",
		"APPROVED_RELEASE_SHA256",
		"readlink -f /opt/vgxness-syncd/current",
		"sha256sum /opt/vgxness-syncd/current/vgxness-syncd")
	assertSectionOrdered(t, filepath.Join(repository, "deploy", "ubuntu", "README.md"), "## Install, validation, and smoke", "\n## Immutable update and rollback",
		"systemctl is-active vgxness-syncd", "%{http_code}", "http://127.0.0.1:8787/v1/sync/capabilities", "systemctl enable --now vgxness-syncd-backup.timer")
	assertExcludesOutsideSection(t, filepath.Join(repository, "deploy", "ubuntu", "README.md"), "## Legacy Caddy retirement (upgrade only)", "\n## Backup and restore", "Caddy")
	makefile, err := os.ReadFile(filepath.Join(repository, "Makefile"))
	if err != nil || !strings.Contains(string(makefile), "TestCleanCheckoutSetupAndNativeSDD|TestUbuntuDeployPackageContract") {
		t.Error("Makefile verify target does not select the Ubuntu deployment contract")
	}
	if _, err := os.Stat(filepath.Join(repository, "deploy", "ubuntu", "Caddyfile.example")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("deploy/ubuntu/Caddyfile.example must be absent, got %v", err)
	}
	for relative, required := range assets {
		data, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		text := string(data)
		for _, want := range required {
			if !strings.Contains(text, want) {
				t.Errorf("%s is missing %q", relative, want)
			}
		}
		for _, forbidden := range forbiddenDeployText(relative) {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains forbidden %q", relative, forbidden)
			}
		}
	}
	docs, err := os.ReadFile(filepath.Join(repository, "docs", "sync.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(docs), "[Ubuntu 24.04 single-VPS deployment package](../deploy/ubuntu/README.md)") {
		t.Error("docs/sync.md does not link the Ubuntu deployment package")
	}
}

func assertMinOccurrences(t *testing.T, path, want string, minimum int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), want); count < minimum {
		t.Errorf("%s contains %d occurrences of %q, want at least %d", path, count, want, minimum)
	}
}

func assertExcludesOutsideSection(t *testing.T, path, section, endDelimiter, forbidden string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, section)
	if start < 0 {
		t.Errorf("%s is missing section %q", path, section)
		return
	}
	end := strings.Index(text[start:], endDelimiter)
	if end < 0 {
		t.Errorf("%s is missing section end delimiter %q", path, endDelimiter)
		return
	}
	if strings.Contains(text[:start], forbidden) || strings.Contains(text[start+end:], forbidden) {
		t.Errorf("%s contains %q outside %q", path, forbidden, section)
	}
}

func assertSectionOrdered(t *testing.T, path, section, endDelimiter string, parts ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sectionOffset := strings.Index(string(data), section)
	if sectionOffset < 0 {
		t.Errorf("%s is missing section %q", path, section)
		return
	}
	sectionText := string(data)[sectionOffset:]
	endOffset := strings.Index(sectionText, endDelimiter)
	if endOffset < 0 {
		t.Errorf("%s is missing section end delimiter %q", path, endDelimiter)
		return
	}
	sectionText = sectionText[:endOffset]
	last := -1
	for _, part := range parts {
		position := strings.Index(sectionText, part)
		if position < 0 || position <= last {
			t.Errorf("%s does not order %q after its predecessor in %q", path, part, section)
		}
		last = position
	}
}

func assertOrdered(t *testing.T, path string, parts ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, part := range parts {
		position := strings.Index(string(data), part)
		if position < 0 || position <= last {
			t.Errorf("%s does not order %q after its predecessor", path, part)
		}
		last = position
	}
}

func forbiddenDeployText(relative string) []string {
	switch relative {
	case "deploy/ubuntu/postgresql-bootstrap.sql.example":
		return []string{"PASSWORD '", "REPLACE_WITH_GENERATED_PASSWORD"}
	case "deploy/ubuntu/vgxness-syncd-backup.service":
		return []string{"ExecStart=/bin/sh -", "sha256sum .current.pgd.tmp |", "rm -rf", "find ", "postgres://", "User=vgxness-syncd\n", "Group=vgxness-syncd\n"}
	case "deploy/ubuntu/vgxness-syncd.service":
		return []string{"0.0.0.0", "Authorization=", "/usr/local/bin/vgxness-syncd"}
	case "deploy/ubuntu/README.md":
		return []string{"does not install packages, create accounts, contact a host", "\ndropdb --force ", "\ncreatedb --owner=", "\npg_restore --exit-on-error", "sha256sum .current.pgd.tmp |", "/healthz", "https://SYNC_PUBLIC_HOSTNAME", "systemctl enable --now caddy"}
	default:
		return nil
	}
}
