package contracts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	schemaassets "github.com/vgxness/vgxness/docs/schemas"
)

const (
	schemaBaseURI             = "https://vgxness.dev/schemas/"
	CommonSchemaURI           = schemaBaseURI + "common.schema.json"
	OrchestrationURI          = schemaBaseURI + "orchestration.schema.json"
	ExecutionSchemaURI        = schemaBaseURI + "execution.schema.json"
	CurrentRunSchemaURI       = schemaBaseURI + "current-run.schema.json"
	RunSchemaURI              = schemaBaseURI + "run.schema.json"
	RunEventSchemaURI         = schemaBaseURI + "run-event.schema.json"
	SkillsSchemaURI           = schemaBaseURI + "skills.schema.json"
	AgentsSchemaURI           = schemaBaseURI + "agents.schema.json"
	PromptsSchemaURI          = schemaBaseURI + "prompts.schema.json"
	DeliveryManifestSchemaURI = schemaBaseURI + "delivery-manifest.schema.json"
	DeliveryReceiptSchemaURI  = schemaBaseURI + "delivery-receipt.schema.json"
	DeliveryCurrentSchemaURI  = schemaBaseURI + "delivery-current.schema.json"
)

var (
	ErrUnknownSchema = errors.New("unknown schema")
	schemaFiles      = []string{
		"common.schema.json",
		"orchestration.schema.json",
		"execution.schema.json",
		"current-run.schema.json",
		"run.schema.json",
		"run-event.schema.json",
		"skills.schema.json",
		"agents.schema.json",
		"bridge.schema.json",
		"prompts.schema.json",
		"delivery-manifest.schema.json",
		"delivery-receipt.schema.json",
		"delivery-current.schema.json",
	}
	defaultKernel = sync.OnceValues(NewKernel)
)

// ContractError is the provider-neutral failure returned at a contract
// boundary. It intentionally omits the rejected document and its values.
type ContractError struct {
	Kind          string `json:"kind"`
	SchemaVersion string `json:"schemaVersion"`
	Code          string `json:"code"`
	SchemaURI     string `json:"schemaUri"`
	Pointer       string `json:"pointer"`
	Message       string `json:"message"`
	Recoverable   bool   `json:"recoverable"`
}

func (e *ContractError) Error() string {
	return fmt.Sprintf("%s: schema=%s pointer=%s: %s", e.Code, e.SchemaURI, e.Pointer, e.Message)
}

func (e *ContractError) Unwrap() error { return ErrInvalid }

// Kernel compiles and validates the embedded contract set. Compilation and
// fragment lookup are serialized; compiled schemas are safe for concurrent use.
type Kernel struct {
	compiler *jsonschema.Compiler
	mu       sync.Mutex
	schemas  map[string]*jsonschema.Schema
	roots    map[string]struct{}
}

// NewKernel loads every schema before compiling any of them so relative refs
// always resolve from the canonical stable URI set.
func NewKernel() (*Kernel, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.UseLoader(offlineLoader{})

	kernel := &Kernel{
		compiler: compiler,
		schemas:  make(map[string]*jsonschema.Schema, len(schemaFiles)),
		roots:    make(map[string]struct{}, len(schemaFiles)),
	}
	for _, file := range schemaFiles {
		data, err := schemaassets.Files.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("load contract schema %s: %w", file, err)
		}
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("parse contract schema %s: %w", file, err)
		}
		uri := schemaBaseURI + file
		if err := compiler.AddResource(uri, document); err != nil {
			return nil, fmt.Errorf("register contract schema %s: %w", uri, err)
		}
		kernel.roots[uri] = struct{}{}
	}
	for root := range kernel.roots {
		schema, err := compiler.Compile(root)
		if err != nil {
			return nil, fmt.Errorf("compile contract schema %s: %w", root, err)
		}
		kernel.schemas[root] = schema
	}
	return kernel, nil
}

// Validate applies the requested root schema or stable fragment. Recoverable
// is supplied by the boundary because only the caller knows whether retry is safe.
func (k *Kernel) Validate(ctx context.Context, schemaURI string, document []byte, recoverable bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	schema, err := k.schema(schemaURI)
	if err != nil {
		return err
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(document))
	if err != nil {
		return contractFailure(schemaURI, "", "document is not valid JSON", recoverable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := schema.Validate(value); err != nil {
		return validationFailure(schemaURI, err, recoverable)
	}
	return ctx.Err()
}

// Validate uses the process-wide embedded kernel.
func Validate(ctx context.Context, schemaURI string, document []byte, recoverable bool) error {
	kernel, err := defaultKernel()
	if err != nil {
		return fmt.Errorf("initialize contract validation: %w", err)
	}
	return kernel.Validate(ctx, schemaURI, document, recoverable)
}

func (k *Kernel) schema(schemaURI string) (*jsonschema.Schema, error) {
	parsed, err := url.Parse(schemaURI)
	if err != nil || parsed.RawQuery != "" || parsed.User != nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSchema, schemaURI)
	}
	parsed.Fragment = ""
	if _, ok := k.roots[parsed.String()]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSchema, schemaURI)
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	if schema := k.schemas[schemaURI]; schema != nil {
		return schema, nil
	}
	schema, err := k.compiler.Compile(schemaURI)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %v", ErrUnknownSchema, schemaURI, err)
	}
	k.schemas[schemaURI] = schema
	return schema, nil
}

func validationFailure(schemaURI string, err error, recoverable bool) error {
	var validationErr *jsonschema.ValidationError
	if !errors.As(err, &validationErr) {
		return contractFailure(schemaURI, "", "document does not satisfy schema", recoverable)
	}
	leaf := validationErr
	for len(leaf.Causes) > 0 {
		leaf = leaf.Causes[0]
	}
	message := "document does not satisfy schema"
	if leaf.ErrorKind != nil {
		keywordPath := leaf.ErrorKind.KeywordPath()
		if len(keywordPath) > 0 {
			message = fmt.Sprintf("document does not satisfy schema keyword %q", keywordPath[len(keywordPath)-1])
		}
	}
	return contractFailure(schemaURI, jsonPointer(leaf.InstanceLocation), message, recoverable)
}

func contractFailure(schemaURI, pointer, message string, recoverable bool) error {
	return &ContractError{
		Kind:          "contract.invalid",
		SchemaVersion: "1",
		Code:          "contract.invalid",
		SchemaURI:     schemaURI,
		Pointer:       pointer,
		Message:       message,
		Recoverable:   recoverable,
	}
}

func jsonPointer(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	escaped := make([]string, len(tokens))
	for index, token := range tokens {
		escaped[index] = strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
	}
	return "/" + strings.Join(escaped, "/")
}

type offlineLoader struct{}

func (offlineLoader) Load(uri string) (any, error) {
	return nil, fmt.Errorf("external schema loading disabled for %s", uri)
}
