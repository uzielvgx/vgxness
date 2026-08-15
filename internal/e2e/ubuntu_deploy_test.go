//go:build e2e

package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUbuntuDeployPackageContract(t *testing.T) {
	repository := repositoryRoot(t)
	assets := map[string][]string{
		"deploy/ubuntu/Caddyfile.example": {
			"https://SYNC_PUBLIC_HOSTNAME {", "reverse_proxy 127.0.0.1:8787",
		},
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
			"pg_dump --format=custom --no-owner --no-privileges --file=/var/lib/vgxness-syncd-backup/.current.pgd.tmp",
			"pg_restore --list /var/lib/vgxness-syncd-backup/.current.pgd.tmp", "sync -f /var/lib/vgxness-syncd-backup/.current.pgd.tmp",
			"sha256sum /var/lib/vgxness-syncd-backup/.current.pgd.tmp", "mv -T -- /var/lib/vgxness-syncd-backup/.current.pgd.tmp /var/lib/vgxness-syncd-backup/current.pgd",
			"sync -f /var/lib/vgxness-syncd-backup/current.pgd",
		},
		"deploy/ubuntu/vgxness-syncd-backup.timer": {
			"OnCalendar=daily", "Persistent=true",
		},
		"deploy/ubuntu/README.md": {
			"Ubuntu 24.04", "authorized root operator", "DBA/maintenance approval", "0600", "systemd-analyze verify",
			"caddy validate", "systemctl daemon-reload", "systemctl reload caddy", "systemctl restart vgxness-syncd",
			"is-active", "ss -ltn", "401", "Preflight", "Abort", "Blast radius", "Restore", "Rollback", "Escalation",
			"[A-Za-z0-9._~-]", ">=32", "/opt/vgxness-syncd/current/vgxness-syncd", "vgxness_syncd_backup",
			"--single-transaction", "--role=vgxness_syncd", "device issue", "device revoke", "/v1/sync/capabilities",
			"getent passwd vgxness-syncd", "getent group vgxness-syncd-backup", "useradd --system", "/usr/sbin/nologin",
			"root:vgxness-syncd-backup", "0750", "root:vgxness-syncd-backup` and mode `0640", "dropdb --force vgxness_sync",
			"createdb --owner=vgxness_syncd vgxness_sync", "INVESTIGATION_VERIFIED.pgd", "STAGED_UNIT_DIRECTORY",
			"files do not execute actions by themselves", "authorized operator executes the reviewed steps",
		},
	}
	assertOrdered(t, filepath.Join(repository, "deploy", "ubuntu", "vgxness-syncd-backup.service"),
		"sync -f /var/lib/vgxness-syncd-backup/.current.pgd.tmp",
		"mv -T -- /var/lib/vgxness-syncd-backup/.current.pgd.tmp /var/lib/vgxness-syncd-backup/current.pgd",
		"sync -f /var/lib/vgxness-syncd-backup/current.pgd")
	makefile, err := os.ReadFile(filepath.Join(repository, "Makefile"))
	if err != nil || !strings.Contains(string(makefile), "TestCleanCheckoutSetupAndNativeSDD|TestUbuntuDeployPackageContract") {
		t.Error("Makefile verify target does not select the Ubuntu deployment contract")
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
		return []string{"/bin/sh -", "rm -rf", "find ", "postgres://", "User=vgxness-syncd\n", "Group=vgxness-syncd\n"}
	case "deploy/ubuntu/vgxness-syncd.service":
		return []string{"0.0.0.0", "Authorization=", "/usr/local/bin/vgxness-syncd"}
	case "deploy/ubuntu/Caddyfile.example":
		return []string{"0.0.0.0", "Authorization"}
	case "deploy/ubuntu/README.md":
		return []string{"does not install packages, create accounts, contact a host"}
	default:
		return nil
	}
}
