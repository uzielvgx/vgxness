.PHONY: fast verify vuln codex-e2e pi-check eval-check

PYTHON ?= python3

eval-check:
	$(PYTHON) -c "import sys; sys.exit(0 if sys.version_info >= (3, 11) else 'Python 3.11+ required; run make eval-check PYTHON=python3.11')"
	$(PYTHON) -m unittest discover -s tools/agent_eval -p 'test_*.py'

pi-check:
	npm ci --ignore-scripts
	npm run typecheck --workspace packages/pi
	npm test --workspace packages/pi
	npm run build --workspace packages/pi

fast:
	test -z "$$(gofmt -l .)"
	go test -short ./...

verify:
	test -z "$$(gofmt -l .)"
	go test -count=1 -covermode=atomic -coverprofile="$${TMPDIR:-/tmp}/vgxness-coverage.out" ./...
	go test -count=1 -race ./...
	go vet ./...
	go mod tidy -diff
	git diff --check
	go mod verify
	go build -trimpath ./...
	go test -tags=e2e -count=1 -run '^(TestCleanCheckoutSetupAndMemory|TestUbuntuDeployPackageContract|TestDockerDeployPackageContract)$$' ./internal/e2e
	GOOS=windows GOARCH=amd64 go build -trimpath ./...
	GOOS=windows GOARCH=arm64 go build -trimpath ./...
	GOOS=windows GOARCH=amd64 go test -count=1 -run '^$$' -exec=/usr/bin/true ./...
	GOOS=windows GOARCH=amd64 go test -tags=e2e -count=1 -run '^$$' -exec=/usr/bin/true ./internal/e2e

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

codex-e2e:
	test "$${VGXNESS_CODEX_E2E:-}" = 1
	go test -tags='e2e codex_e2e' -count=1 -run '^TestCodexDelegationRuntime$$' ./internal/e2e
