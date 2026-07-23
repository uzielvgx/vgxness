package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/delivery"
)

const maxDeliveryManifestBytes = 1 << 20

func runDelivery(ctx context.Context, args []string, stdout, stderr io.Writer, runtime delivery.Runtime) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: vgxness delivery <issue|status|validate|invalidate>")
		return 2
	}
	if runtime == nil {
		fmt.Fprintln(stderr, "operational: delivery authority is unavailable")
		return 1
	}
	command := args[0]
	flags := flag.NewFlagSet("delivery "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options config.Options
	var manifestPath, baseRef, gate, receiptID, reason string
	flags.StringVar(&options.StorageRoot, "storage-root", "", "storage root")
	flags.BoolVar(&options.ProjectLocal, "project-local", false, "use project-local storage")
	switch command {
	case "issue":
		flags.StringVar(&manifestPath, "manifest", "", "review manifest JSON")
		flags.StringVar(&baseRef, "base-ref", "HEAD", "review base commit")
	case "status":
	case "validate":
		flags.StringVar(&manifestPath, "manifest", "", "same review manifest JSON")
		flags.StringVar(&baseRef, "base-ref", "", "optional expected base commit")
		flags.StringVar(&gate, "gate", "", "post-apply, pre-commit, pre-push, or pre-pr")
		flags.StringVar(&receiptID, "receipt", "", "optional exact current receipt ID")
	case "invalidate":
		flags.StringVar(&reason, "reason", "", "explicit invalidation reason")
	default:
		fmt.Fprintln(stderr, "usage: vgxness delivery <issue|status|validate|invalidate>")
		return 2
	}
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "invalid delivery arguments")
		return 2
	}

	var result any
	var err error
	switch command {
	case "issue":
		manifest, readErr := readDeliveryManifest(manifestPath)
		if readErr != nil {
			err = readErr
			break
		}
		result, err = runtime.Issue(ctx, options, delivery.IssueRequest{BaseRef: baseRef, Manifest: manifest})
	case "status":
		result, err = runtime.Status(ctx, options)
	case "validate":
		manifest, readErr := readDeliveryManifest(manifestPath)
		if readErr != nil {
			err = readErr
			break
		}
		result, err = runtime.Validate(ctx, options, delivery.ValidateRequest{Gate: delivery.Gate(gate), BaseRef: baseRef, ReceiptID: receiptID, Manifest: manifest})
	case "invalidate":
		result, err = runtime.Invalidate(ctx, options, reason)
	}
	if err != nil {
		code, message := failure(err)
		fmt.Fprintln(stderr, message)
		return code
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(stderr, "io: delivery output failed")
		return 1
	}
	return 0
}

func readDeliveryManifest(path string) (delivery.Manifest, error) {
	if strings.TrimSpace(path) == "" {
		return delivery.Manifest{}, fmt.Errorf("%w: --manifest is required", delivery.ErrInvalid)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return delivery.Manifest{}, fmt.Errorf("%w: manifest path", delivery.ErrInvalid)
	}
	file, err := os.Open(abs)
	if err != nil {
		return delivery.Manifest{}, fmt.Errorf("%w: open manifest", delivery.ErrInvalid)
	}
	defer file.Close()
	opened, err := file.Stat()
	current, pathErr := os.Lstat(abs)
	if err != nil || pathErr != nil || !opened.Mode().IsRegular() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, current) || opened.Size() > maxDeliveryManifestBytes {
		return delivery.Manifest{}, fmt.Errorf("%w: manifest must be a bounded regular file", delivery.ErrInvalid)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDeliveryManifestBytes+1))
	if err != nil || len(data) > maxDeliveryManifestBytes {
		return delivery.Manifest{}, fmt.Errorf("%w: manifest exceeds its read bound", delivery.ErrInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest delivery.Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return delivery.Manifest{}, fmt.Errorf("%w: decode manifest", delivery.ErrInvalid)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return delivery.Manifest{}, fmt.Errorf("%w: trailing manifest data", delivery.ErrInvalid)
	}
	return manifest, nil
}
