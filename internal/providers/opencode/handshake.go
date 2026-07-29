package opencode

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
)

const (
	minimumVersion   = "1.18.4"
	handshakeTimeout = 15 * time.Second
	versionOutputMax = 4096
)

type Prober struct {
	executable string
	lookPath   func(string) (string, error)
	run        func(context.Context, string, string) ([]byte, error)
}

func NewProber(executable string) *Prober {
	return &Prober{executable: executable, lookPath: exec.LookPath, run: runVersion}
}

func (prober *Prober) Probe(ctx context.Context, workspace string) (integration.Handshake, error) {
	unavailable := integration.Handshake{Status: integration.HandshakeUnavailable}
	if err := ctx.Err(); err != nil {
		return unavailable, err
	}
	if prober == nil || prober.lookPath == nil || prober.run == nil || !filepath.IsAbs(workspace) {
		return unavailable, nil
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return unavailable, nil
	}
	workspace, err = filepath.EvalSymlinks(filepath.Clean(workspace))
	if err != nil || !filepath.IsAbs(workspace) {
		return unavailable, nil
	}
	executable := strings.TrimSpace(prober.executable)
	if executable == "" {
		executable = "opencode"
	}
	executable, err = prober.lookPath(executable)
	if err != nil {
		return unavailable, nil
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return unavailable, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	output, err := prober.run(probeCtx, executable, workspace)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return unavailable, ctxErr
	}
	if err != nil {
		return unavailable, nil
	}
	version, ok := parseHandshakeVersion(string(output))
	minimum, minimumOK := parseHandshakeVersion(minimumVersion)
	if !ok || !minimumOK || version[0] != 1 || compareHandshakeVersion(version, minimum) < 0 {
		return integration.Handshake{Status: integration.HandshakeIncompatible}, nil
	}
	return integration.Handshake{OK: true, Status: integration.HandshakeHealthy}, nil
}

func runVersion(ctx context.Context, executable, workspace string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, "--version")
	command.Dir = workspace
	command.WaitDelay = time.Second
	output := limitedBuffer{limit: versionOutputMax}
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil || output.overflow {
		return nil, errUnavailable
	}
	return bytes.Clone(output.Bytes()), nil
}

var errUnavailable = &probeError{}

type probeError struct{}

func (*probeError) Error() string { return "OpenCode unavailable" }

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := buffer.limit - buffer.Len()
	if remaining <= 0 {
		buffer.overflow = true
		return written, nil
	}
	if len(value) > remaining {
		buffer.overflow = true
		value = value[:remaining]
	}
	_, _ = buffer.Buffer.Write(value)
	return written, nil
}

type handshakeVersion [3]int

func parseHandshakeVersion(value string) (handshakeVersion, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	if fields := strings.Fields(value); len(fields) > 0 {
		value = fields[0]
	}
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		value = value[:index]
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return handshakeVersion{}, false
	}
	var version handshakeVersion
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return handshakeVersion{}, false
		}
		version[index] = number
	}
	return version, true
}

func compareHandshakeVersion(left, right handshakeVersion) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}
