package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestGoCIWorkflowContract(t *testing.T) {
	workflow := readRepositoryFile(t, "../../.github/workflows/go-ci.yml")
	t.Run("workflow is auditable", func(t *testing.T) {
		for _, want := range []string{
			"workflow_call:", "permissions:\n  contents: read", "pull_request:", "push:", "branches: [main]",
			"go-version: 1.26.6", "persist-credentials: false", "cancel-in-progress: true",
			"ref: ${{ inputs.ref || github.sha }}",
			"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
			"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0",
			"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1",
		} {
			if !strings.Contains(workflow, want) {
				t.Errorf("workflow missing %q", want)
			}
		}
		if strings.Count(workflow, "branches: [main]") != 2 {
			t.Error("workflow must target main for pull requests and pushes")
		}
		if strings.Contains(workflow, "go-version: 1.26.3") {
			t.Error("workflow must not use stale Go 1.26.3")
		}
		if got, want := strings.Count(workflow, "go-version: 1.26.6"), strings.Count(workflow, "actions/setup-go@"); got != want {
			t.Errorf("workflow must configure Go 1.26.6 in every setup-go step: got %d, want %d", got, want)
		}
		pinned := regexp.MustCompile(`^[^@]+@[0-9a-f]{40} # v[0-9]+\.[0-9]+\.[0-9]+$`)
		for _, line := range strings.Split(workflow, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "uses: ") && !pinned.MatchString(strings.TrimPrefix(line, "uses: ")) {
				t.Errorf("mutable or unauditable action: %q", line)
			}
		}
	})
	t.Run("standard lanes are independent and aggregated", func(t *testing.T) {
		lanes := []string{"coverage", "postgres-integration", "race", "static", "vulnerability", "linux-e2e", "fuzz-openspec", "fuzz-launcher-manifest", "windows-compile", "windows-install", "darwin-smoke"}
		for _, lane := range lanes {
			if !strings.Contains(workflow, "  "+lane+":\n") {
				t.Errorf("workflow missing standard lane %q", lane)
			}
		}
		if strings.Count(workflow, "    needs:") != 1 || !strings.Contains(workflow, "needs: [coverage, postgres-integration, race, static, vulnerability, linux-e2e, fuzz-openspec, fuzz-launcher-manifest, windows-compile, windows-install, darwin-smoke]") {
			t.Error("only the standard aggregate gate may depend on validation lanes")
		}
		if !strings.Contains(workflow, "  quality:\n    name: quality\n    if: ${{ always() }}") {
			t.Error("workflow must preserve the always-running quality check required by branch protection")
		}
		for _, result := range []string{"needs.coverage.result", "needs.postgres-integration.result", "needs.race.result", "needs.static.result", "needs.vulnerability.result", "needs.linux-e2e.result", "needs.fuzz-openspec.result", "needs.fuzz-launcher-manifest.result", "needs.windows-compile.result", "needs.windows-install.result", "needs.darwin-smoke.result"} {
			if !strings.Contains(workflow, result) {
				t.Errorf("aggregate gate does not require %q", result)
			}
		}
	})
	t.Run("PostgreSQL evidence is durable", func(t *testing.T) {
		start := strings.Index(workflow, "  postgres-integration:\n")
		end := strings.Index(workflow, "  race:\n")
		if start < 0 || end <= start {
			t.Fatal("workflow must define the PostgreSQL integration lane before race")
		}
		lane := workflow[start:end]
		if !strings.Contains(lane, "image: postgres:17") {
			t.Error("PostgreSQL integration lane must pin the service image to postgres:17")
		}
		upload := strings.Index(lane, "- name: Upload PostgreSQL test evidence")
		if upload < 0 || !strings.Contains(lane[upload:], "if: always()") || !strings.Contains(lane[upload:], "path: postgres-test.json") {
			t.Error("PostgreSQL test evidence upload must always publish postgres-test.json")
		}
	})
	t.Run("vulnerability scanning is pinned and isolated", func(t *testing.T) {
		vulnerability := strings.Index(workflow, "  vulnerability:\n")
		quality := strings.Index(workflow, "  quality:\n")
		if vulnerability < 0 || quality <= vulnerability {
			t.Fatal("workflow must define a vulnerability lane before the quality gate")
		}
		var laneLines []string
		for _, line := range strings.Split(workflow[vulnerability:], "\n") {
			if len(laneLines) > 0 && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
				break
			}
			laneLines = append(laneLines, line)
		}
		lane := strings.Join(laneLines, "\n")
		qualityGate := workflow[quality:]
		for _, want := range []string{
			"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
			"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0",
			"run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
		} {
			if !strings.Contains(lane, want) {
				t.Errorf("vulnerability lane missing %q", want)
			}
		}
		for _, want := range []string{
			"VULNERABILITY_RESULT: ${{ needs.vulnerability.result }}",
			`require_success vulnerability "$VULNERABILITY_RESULT"`,
			`printf '%s lane result: %s\n' "$lane" "$result"`,
		} {
			if !strings.Contains(qualityGate, want) {
				t.Errorf("quality gate does not consume vulnerability evidence %q", want)
			}
		}

		makefile := readRepositoryFile(t, "../../Makefile")
		const target = "\nvuln:\n\tgo run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./..."
		if !strings.Contains(makefile, ".PHONY: fast verify vuln") || !strings.Contains(makefile, target) {
			t.Error("Makefile must expose the exact pinned vulnerability scan as a separate vuln target")
		}
		verify := strings.Index(makefile, "\nverify:\n")
		vuln := strings.Index(makefile, "\nvuln:\n")
		if verify < 0 || vuln < 0 {
			t.Fatal("Makefile must retain verify and vuln targets")
		}
		verifyRecipe := makefile[verify:]
		if vuln > verify {
			verifyRecipe = makefile[verify:vuln]
		}
		if strings.Contains(verifyRecipe, "govulncheck") {
			t.Error("network-dependent vulnerability scanning must remain outside make verify")
		}
	})
	t.Run("all standard evidence is declared", func(t *testing.T) {
		for _, command := range []string{
			"go test -count=1 -covermode=atomic -coverprofile=coverage.out ./...", "go test -count=1 -race ./...",
			"test -n \"${VGXNESS_TEST_POSTGRES_DSN:-}\"", "go test -json -count=1 ./internal/syncpg ./cmd/vgxness-syncd",
			"postgres-test.json", "\\\"Action\\\":\\\"skip\\\"", "PostgreSQL test output contains a skipped test or package",
			"go tool cover -func=coverage.out", "required=74.5", "Coverage floor failed:",
			"go vet ./...", "gofmt -l .", "go mod tidy -diff", "git diff --check",
			"go mod verify", "go build -trimpath ./...",
			"go test -tags=e2e -count=1 -run '^TestCleanCheckoutSetupAndNativeSDD$' ./internal/e2e",
			"go test -count=1 -run '^$' -fuzz '^FuzzParseOpenSpecProjection$' -fuzztime=10s ./internal/sdd",
			"go test -count=1 -run '^$' -fuzz '^FuzzDecodeManifest$' -fuzztime=10s ./internal/launcher",
			"GOOS=windows GOARCH=amd64 go test -count=1 -run '^$' -exec=/usr/bin/true ./...",
			"GOOS=windows GOARCH=amd64 go test -tags=e2e -count=1 -run '^$' -exec=/usr/bin/true ./internal/e2e",
			"go test -count=1 ./...",
			"go build -trimpath -o vgxness ./cmd/vgxness", "./vgxness version",
			"go test -count=1 ./internal/launcher ./internal/config ./internal/memory ./internal/selfinstall ./internal/providers/...",
		} {
			if !strings.Contains(workflow, command) {
				t.Errorf("workflow missing gate %q", command)
			}
		}
		if strings.Contains(workflow, "run: go test ./...") {
			t.Error("coverage is the ordinary test evidence; an uncounted duplicate plain full-test pass is forbidden")
		}
		if strings.Contains(workflow, "go mod tidy\n") || strings.Contains(workflow, "go test -c -o") || strings.Contains(workflow, "while IFS=") {
			t.Error("workflow contains a mutating tidy or serial Windows test compilation")
		}
		if strings.Count(workflow, "run: go mod download") != 8 {
			t.Error("the eight cold-runner test and smoke jobs must prefetch modules")
		}
	})
	t.Run("coverage upload survives failure", func(t *testing.T) {
		upload := strings.Index(workflow, "- name: Upload coverage")
		race := strings.Index(workflow, "  race:")
		if upload < 0 || race <= upload || !strings.Contains(workflow[upload:race], "if: always()") || !strings.Contains(workflow[upload:race], "path: coverage.out") {
			t.Error("coverage upload must always run and publish coverage.out")
		}
	})
	t.Run("darwin smoke uses the current standard runner", func(t *testing.T) {
		if !strings.Contains(workflow, "  darwin-smoke:\n    runs-on: macos-15") {
			t.Error("Darwin source smoke must run natively on the standard macos-15 ARM64 runner")
		}
		if strings.Contains(workflow, "macos-14") {
			t.Error("Go CI must not use the deprecated macos-14 runner")
		}
		if !strings.Contains(workflow, "go test -count=1 ./internal/launcher ./internal/config ./internal/memory ./internal/selfinstall ./internal/providers/...") {
			t.Error("Darwin native package command must include self-install coverage")
		}
	})
}

func TestReleaseWorkflowContract(t *testing.T) {
	workflow := readRepositoryFile(t, "../../.github/workflows/release.yml")
	for _, want := range []string{
		"uses: ./.github/workflows/go-ci.yml", "ref: ${{ github.sha }}", "  build:\n    runs-on: ubuntu-24.04",
		"release_commit: ${{ steps.release_metadata.outputs.commit }}", "release_date: ${{ steps.release_metadata.outputs.date }}", "id: release_metadata",
		"  darwin-smoke:\n    needs: build\n    runs-on: macos-15", "needs: [standard-validation, build, windows-smoke, darwin-smoke]",
		"contents: write", "id-token: write", "attestations: write", "sha256sum -c SHA256SUMS",
		"go-version: 1.26.6",
		"Verify Linux artifact and self-install", "Verify Windows artifact and self-install", "Verify Darwin artifact and self-install", "--verify-tag", "--prerelease",
		"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
		"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1",
		"actions/download-artifact@37930b1c2abaa49bbe596cd826c3c89aef350131 # v7.0.0",
		"actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6 # v4.2.2", "subject-path: dist/*",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
	for _, asset := range []string{
		"linux_amd64.tar.gz", "linux_arm64.tar.gz", "darwin_amd64.tar.gz", "darwin_arm64.tar.gz",
		"windows_amd64.zip", "windows_arm64.zip", "dist/SHA256SUMS",
	} {
		if strings.Count(workflow, asset) == 0 {
			t.Errorf("release workflow missing asset %q", asset)
		}
	}
	if strings.Contains(workflow, "Validate tagged source") || strings.Contains(workflow, "run: go test ./...") {
		t.Error("release must reuse exact-SHA standard validation instead of maintaining a partial inline gate")
	}
	if strings.Contains(workflow, "  build:\n    needs: standard-validation") {
		t.Error("release asset construction should overlap standard validation; publication remains the joining gate")
	}
	if strings.Contains(workflow, "go-version: 1.26.3") {
		t.Error("release workflow must not use stale Go 1.26.3")
	}
	darwin := strings.Index(workflow, "  darwin-smoke:\n")
	publish := strings.Index(workflow, "  publish:\n")
	if darwin < 0 || publish <= darwin {
		t.Fatal("release workflow must define Darwin artifact smoke before publish")
	}
	darwinWorkflow := workflow[darwin:publish]
	for _, want := range []string{
		"name: release-dist-${{ github.ref_name }}", "path: dist", `if [[ "$machine" != arm64 ]]; then`,
		`printf 'expected macOS ARM64 runner, got %s\n' "$machine" >&2`, "target=arm64",
		"RELEASE_COMMIT: ${{ needs.build.outputs.release_commit }}", "RELEASE_DATE: ${{ needs.build.outputs.release_date }}",
		`archive="vgxness_${version}_darwin_${target}.tar.gz"`,
		`awk -v archive="$archive" 'NF == 2 && $2 == archive { print }' dist/SHA256SUMS > "$checksum_file"`,
		`if [[ "$(wc -l < "$checksum_file" | tr -d ' ')" != 1 ]]; then`, `(cd dist && shasum -a 256 -c "$checksum_file")`,
		`extract="$(mktemp -d)"`, `install="$(mktemp -d)"`, `tar -xzf "dist/$archive" -C "$extract"`,
		`version_output="$("$binary" version)"`, `grep -Fx "version=$GITHUB_REF_NAME" <<<"$version_output"`,
		`grep -Fx "commit=$RELEASE_COMMIT" <<<"$version_output"`, `grep -Fx "date=$RELEASE_DATE" <<<"$version_output"`,
		`grep -Fx "os=darwin" <<<"$version_output"`, `grep -Fx "arch=$target" <<<"$version_output"`,
		`preview="$("$binary" self preview --bin-dir "$install/bin" --data-dir "$install/data")"`,
		`install_output="$("$binary" self install --bin-dir "$install/bin" --data-dir "$install/data")"`,
		`status="$("$install/bin/vgxness" self status --bin-dir "$install/bin" --data-dir "$install/data")"`,
		`grep -Fx "state=absent" <<<"$preview"`, `grep -Fx "state=installed" <<<"$install_output"`, `grep -Fx "state=installed" <<<"$status"`,
	} {
		if !strings.Contains(darwinWorkflow, want) {
			t.Errorf("Darwin release smoke missing %q", want)
		}
	}
	if regexp.MustCompile(`(?m)\b(?:sleep|retry|retries)\b`).MatchString(darwinWorkflow) {
		t.Error("Darwin release smoke must not hide failures with sleeps or retries")
	}
	publishWorkflow := workflow[publish:]
	for _, permission := range []string{"      contents: write", "      id-token: write", "      attestations: write"} {
		if !strings.Contains(publishWorkflow, permission) {
			t.Errorf("publish job missing permission %q", permission)
		}
	}
	checksum := strings.Index(publishWorkflow, "sha256sum -c SHA256SUMS")
	attest := strings.Index(publishWorkflow, "actions/attest@1e69f48acb82d1966a394da916b4c1698aa569d6 # v4.2.2")
	release := strings.Index(publishWorkflow, "gh release create")
	if checksum < 0 || attest < 0 || release < 0 || !(checksum < attest && attest < release) {
		t.Error("publish must verify checksums before attesting dist/* and creating the GitHub release")
	}
}

func TestDependencyFloors(t *testing.T) {
	goMod := readRepositoryFile(t, "../../go.mod")
	goSum := readRepositoryFile(t, "../../go.sum")
	for _, check := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "go.mod requirement", content: goMod, want: "golang.org/x/text v0.39.0 // indirect"},
		{name: "go.sum module checksum", content: goSum, want: "golang.org/x/text v0.39.0 "},
		{name: "go.sum go.mod checksum", content: goSum, want: "golang.org/x/text v0.39.0/go.mod "},
	} {
		if !strings.Contains(check.content, check.want) {
			t.Errorf("dependency floor missing %s %q", check.name, check.want)
		}
	}
}

func TestDocumentedSQLiteSchemaMatchesMigrationHead(t *testing.T) {
	migrations := readRepositoryFile(t, "../memory/migrations.go")
	versions := regexp.MustCompile(`\{version: ([0-9]+),`).FindAllStringSubmatch(migrations, -1)
	if len(versions) == 0 {
		t.Fatal("memory migration ledger has no versions")
	}
	head := versions[len(versions)-1][1]
	for _, path := range []string{"../../README.md", "../../docs/memory.md", "../../docs/go-implementation.md", "../../docs/opencode-integration.md", "../../docs/orchestration-flow.md"} {
		document := readRepositoryFile(t, path)
		if !strings.Contains(document, "schema v"+head) && !strings.Contains(document, "schema-v"+head) {
			t.Errorf("%s does not declare current SQLite schema v%s", path, head)
		}
		for version := 1; version < len(versions); version++ {
			stale := "v" + strconv.Itoa(version)
			phrase := regexp.MustCompile(`(?:schema |schema-|older supported schema to )` + regexp.QuoteMeta(stale) + `(?:\D|$)`)
			if phrase.MatchString(document) {
				t.Errorf("%s retains stale current SQLite schema phrase %q", path, stale)
			}
		}
	}
}

func readRepositoryFile(t *testing.T, path string) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate foundation test source")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), path))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func TestUnreleasedChangelogManagedArtifactVersions(t *testing.T) {
	changelog := readRepositoryFile(t, "../../CHANGELOG.md")
	_, unreleased, ok := strings.Cut(changelog, "## Unreleased\n")
	if !ok {
		t.Fatal("CHANGELOG missing Unreleased section")
	}
	if nextSection := strings.Index(unreleased, "\n## "); nextSection >= 0 {
		unreleased = unreleased[:nextSection]
	}
	for _, current := range []string{
		"17 managed artifacts",
		"vgxness-memory-lifecycle.ts",
	} {
		if !strings.Contains(unreleased, current) {
			t.Errorf("Unreleased section missing current managed artifact fact %q", current)
		}
	}
	for _, earlier := range []string{
		"Earlier in this unreleased cycle, projected the shared adaptive route",
		"16 managed artifacts",
		"manager v58",
		"Codex manager v17",
		"`general` v10",
		"verifier v7",
		"`explore` v4",
	} {
		if !strings.Contains(unreleased, earlier) {
			t.Errorf("Unreleased section missing earlier-cycle managed artifact fact %q", earlier)
		}
	}
	for _, stale := range []string{
		"18 managed artifacts",
		"OpenCode manager v49",
		"Codex manager v9",
		"manager v57",
		"Codex manager v16",
		"`general` v6",
		"verifier v4",
		"`explore` v2",
	} {
		for _, line := range strings.Split(unreleased, "\n") {
			if strings.Contains(line, stale) && !strings.Contains(line, "Earlier in this unreleased cycle") {
				t.Errorf("Unreleased section presents stale managed artifact fact %q as current", stale)
			}
		}
	}
}

func TestDocumentedDockerAdmissionBoundary(t *testing.T) {
	compose := readRepositoryFile(t, "../../deploy/docker/compose.yaml")
	for _, want := range []string{
		"VGXNESS_SYNC_AUTH_GLOBAL_PER_MINUTE: \"120\"",
		"VGXNESS_SYNC_AUTH_DEVICE_PER_MINUTE: \"60\"",
		"VGXNESS_SYNC_AUTH_DEVICE_STATES: \"256\"",
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("Docker deployment omits admission default %q", want)
		}
	}
	for _, path := range []string{"../../deploy/docker/README.md", "../../docs/sync.md"} {
		if !strings.Contains(readRepositoryFile(t, path), "rate limit") {
			t.Errorf("%s omits the distributed admission-limit boundary", path)
		}
	}
}

func TestFoundationProductContract(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	current := string(readme)
	for _, claim := range []string{"Go 1.26", "`status`", "`doctor`", "SQLite", "read-only"} {
		if !strings.Contains(current, claim) {
			t.Errorf("README omits delivered foundation claim %q", claim)
		}
	}
	for _, stale := range []string{"does not contain Go source", "complete product runtime", "Chronicle writer is delivered"} {
		if strings.Contains(current, stale) {
			t.Errorf("README contains unsupported claim %q", stale)
		}
	}
	migrations, err := filepath.Glob("../memory/migrations/*.sql")
	if err != nil || len(migrations) != 21 {
		t.Fatalf("foundation must retain exactly twenty-one migrations: %v %v", migrations, err)
	}
	if err := filepath.WalkDir("../../.github", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.Contains(strings.ToLower(path), "ruleset") || strings.Contains(strings.ToLower(path), "branch-protection") {
			t.Errorf("branch protection remains deferred: %s", path)
		}
		return walkErr
	}); err != nil {
		t.Fatal(err)
	}
	assertOpenCodeDocumentationContract(t)
}

func assertOpenCodeDocumentationContract(t *testing.T) {
	t.Helper()
	documents := map[string][]string{
		"../../README.md":                     {"17 managed artifacts", "13 model-bound agents", "`vgxness-manager` v59", "Codex Manager18", "CARE-v2 Manager59", "CARE-v2 Manager58", "CARE-v1 Manager58 and CARE-v1 Manager57", "git-delivery` v1", "three CARE v2 profiles", "`general` v10", "verifier v7", "zero execution tools", "at most one autonomous save", "not runtime enforcement", "`vgxness mcp --full`", "eight memory and 13 SDD tools", "plugins/vgxness-memory-lifecycle.ts", "no `opencode.json` plugin entry", "no caller identity", "automatic memory injection", "runtime-security guarantee", "vgxness.ts", "v1-v10", "vgxness-autonomous-stacked-pr", "v1/v2/v3", "Modified, malformed, foreign, unknown, or newer bytes block without removal"},
		"../../docs/opencode-integration.md":  {"17 managed artifacts", "13 agents", "OpenCode current is CARE-v2 Manager59", "immediate predecessor is exact CARE-v2 Manager58", "CARE-v1 Manager58 and CARE-v1 Manager57", "OpenCode v56/verifier-v6 is deeper", "`general` v10", "verifier v7", "three CARE v2 roles", "six SDD roles", "zero execution tools", "at most one durable", "not runtime enforcement", "`vgxness mcp --full`", "eight memory and 13 SDD tools", "plugins/vgxness-memory-lifecycle.ts", "no `opencode.json` plugin entry", "missing it is partial", "no caller identity", "automatic memory injection", "schema v21", "vgxness.ts", "v1-v10", "vgxness-autonomous-stacked-pr", "v1/v2/v3", "modified, malformed, foreign, unknown, or newer bytes block without removal"},
		"../../docs/product-blueprint.md":     {"17 provider artifacts", "Managed OpenCode CARE-v2 Manager59 and generated Codex Manager18", "Manager v59", "zero-execution-tool fast path", "at most one autonomous save", "not runtime enforcement", "`vgxness mcp --full`", "eight memory tools and 13 SDD tools", "plugins/vgxness-memory-lifecycle.ts", "no `opencode.json` plugin entry", "No additional installed plugin", "automatic memory injection", "OpenCode manager v57 and Codex manager v16 artifacts remain recognized historical predecessors", "vgxness.ts", "v1-v10", "vgxness-autonomous-stacked-pr", "v1/v2/v3", "modified, malformed, foreign, unknown, or newer bytes block without removal"},
		"../../docs/product-blueprint.es.md":  {"17 artefactos", "OpenCode administrado CARE-v2 Manager59 y Codex generado Manager18", "Manager v59", "ruta rápida sin herramientas de ejecución", "como máximo un guardado autónomo", "no enforcement de runtime", "`vgxness mcp --full`", "ocho herramientas de memoria y 13 de SDD", "plugins/vgxness-memory-lifecycle.ts", "no tiene entrada `plugin` en `opencode.json`", "No hay plugins adicionales", "no tiene identidad del llamador", "inyección automática de memoria", "manager v57 de OpenCode y el manager v16 de Codex", "vgxness.ts", "v1-v10", "vgxness-autonomous-stacked-pr", "v1/v2/v3", "modificados, malformados, extranjeros, desconocidos o más nuevos bloquean sin eliminación"},
		"../../docs/go-implementation.md":     {"17 managed artifacts", "OpenCode current CARE-v2 Manager59 roles", "immediate CARE-v2/Manager58 predecessor", "CARE-v1/Manager58 and CARE-v1/Manager57", "OpenCode v56/verifier-v6 deeper lifecycle identity", "Codex current Manager18", "Codex Manager17 as immediate predecessor", "Manager16 and deeper Manager15/v14", "12 delegated profiles", "schema v21", "not a Go provider runtime or a new schema/transport surface", "`vgxness mcp --full`", "plugins/vgxness-memory-lifecycle.ts", "no `opencode.json` plugin entry", "vgxness.ts", "v1-v10", "vgxness-autonomous-stacked-pr", "v1/v2/v3", "modified, malformed, foreign, unknown, or newer bytes are drift"},
		"../../docs/opencode-setup-wizard.md": {"17 managed artifacts", "Manager59", "OpenCode immediate CARE-v2/Manager58", "CARE-v1/Manager58 and CARE-v1/Manager57", "`general` v10", "verifier v7", "three CARE v2 roles", "six SDD roles", "not a runtime broker", "`vgxness mcp --full`", "eight memory and 13 SDD tools", "plugins/vgxness-memory-lifecycle.ts", "no `opencode.json` plugin entry", "vgxness.ts", "v1-v10", "vgxness-autonomous-stacked-pr", "v1/v2/v3", "modified, malformed, foreign, unknown, or newer bytes block without removal"},
		"../../docs/codex-integration.md":     {"Codex current identity is Manager18", "immediately preceded by exact Manager17", "Manager16/15/14", "OpenCode current identity is CARE-v2 Manager59", "memory_context", "eight memory and 13 SDD tools", "ten mutating tools", "zero execution tools", "at most one autonomous `memory_save`", "rather than Codex runtime enforcement"},
		"../../docs/hooks.md":                 {"exact auto-discovered OpenCode lifecycle plugin", "plugins/vgxness-memory-lifecycle.ts", "no `opencode.json` plugin entry", "top-level session", "bounded isolated context handoff", "transcript-free compaction checkpoint", "summary completion", "completed` or `interrupted", "shell or Git hooks", "broad observability", "automatic memory injection", "no caller identity", "no runtime-security claim"},
	}
	for path, claims := range documents {
		content := readRepositoryFile(t, path)
		currentContent := content
		if path == "../../CHANGELOG.md" {
			currentContent = strings.SplitN(content, "\n## v", 2)[0]
		}
		for _, claim := range claims {
			if !strings.Contains(currentContent, claim) {
				t.Errorf("%s omits OpenCode contract claim %q", path, claim)
			}
		}
		for _, stale := range []string{"Managed OpenCode v58 and generated Codex v17", "OpenCode current CARE v2 with Manager58", "OpenCode current is CARE v2 with Manager58", "OpenCode immediate predecessor is exact CARE-v1 with Manager58", "OpenCode immediate CARE-v1/Manager58 predecessor", "Codex current Manager17", "Codex immediate predecessor is Manager16", "three CARE v1 roles", "manager-v57 package is the immediate predecessor", "Exact `vgxness-manager` v57 and Codex manager v16 artifacts are immediate historical predecessors", "its exact v14 artifact is the immediate predecessor", "19 managed artifacts", "19 provider artifacts", "plugin v9", "dedicated `opencode.jsonc`", "exact `opencode.jsonc` overlay"} {
			if strings.Contains(currentContent, stale) {
				t.Errorf("%s contains stale OpenCode contract claim %q", path, stale)
			}
		}
	}
	integration := readRepositoryFile(t, "../../docs/opencode-integration.md")
	for _, claim := range []string{
		"`vgxness setup opencode` supports `--config-dir` and the model flags",
		"publishes global skills to the default discoverable root",
		"Only lower-level `vgxness skills <preview|install|status|uninstall> --skills-dir PATH` supports isolated custom roots",
	} {
		if !strings.Contains(integration, claim) {
			t.Errorf("opencode integration omits setup/skills-root distinction %q", claim)
		}
	}
	if strings.Contains(integration, "`--skills-dir` selects an absolute global portable-skills directory") {
		t.Error("opencode integration incorrectly advertises --skills-dir on setup")
	}
	for path, stale := range map[string]string{
		"../../README.md":                     "modified, unknown, and newer bytes block",
		"../../docs/opencode-integration.md":  "modified, unknown, or newer bytes block",
		"../../docs/product-blueprint.es.md":  "modificados, desconocidos o más nuevos bloquean",
		"../../docs/hooks.md":                 "modified or unknown bytes block",
		"../../docs/go-implementation.md":     "modified, unknown, and newer bytes block",
		"../../docs/opencode-setup-wizard.md": "modified, unknown, and newer bytes block",
	} {
		if strings.Contains(readRepositoryFile(t, path), stale) {
			t.Errorf("%s contains abbreviated retirement blocking rule %q", path, stale)
		}
	}
}
