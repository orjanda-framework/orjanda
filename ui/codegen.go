package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/orjanda-framework/orjanda/schema"
)

// ContentHash computes a stable hash of the codegen-relevant Registry surface:
// any change to a DocType's metadata that would alter the generated TypeScript
// (fields, types, requiredness, permissions, titles, child tables) changes the
// hash. `orjanda serve` (Phase 10) compares this hash across Registry
// recompiles to decide whether to re-run the codegen pass (TAD §6.3 step 3).
func ContentHash(reg schema.Registry) (string, error) {
	input, err := CodegenInput(reg)
	if err != nil {
		return "", err
	}
	// Canonical ordering regardless of Registry.List order.
	sort.Slice(input, func(i, j int) bool { return input[i].Name < input[j].Name })
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// RegenerateOptions configures a codegen pass (TAD §6.3 step 2).
type RegenerateOptions struct {
	// Registry is the compiled Registry to snapshot.
	Registry schema.Registry
	// ScriptPath is the absolute path to the @orjanda/codegen Node script.
	// Defaults to DefaultScriptPath.
	ScriptPath string
	// InputPath is where the TAD §6.3 step-1 payload is written. Defaults to
	// "orjanda-ui/src/generated/schema.json".
	InputPath string
	// OutputDir is where generated TypeScript is written. Defaults to
	// "orjanda-ui/src/generated".
	OutputDir string
	// MarkerPath records the last ContentHash so unchanged Registries skip
	// regeneration. Defaults to <OutputDir>/.registry-hash.
	MarkerPath string
	// NodeBin is the node binary; defaults to "node".
	NodeBin string
}

// defaultPaths resolves the OutputDir/InputPath/MarkerPath defaults (relative
// to the working directory, TAD §6.3 step 3).
func defaultPaths(opts RegenerateOptions) (outDir, marker, inputPath string) {
	outDir = opts.OutputDir
	if outDir == "" {
		outDir = "orjanda-ui/src/generated"
	}
	marker = opts.MarkerPath
	if marker == "" {
		marker = filepath.Join(outDir, ".registry-hash")
	}
	inputPath = opts.InputPath
	if inputPath == "" {
		inputPath = filepath.Join(outDir, "schema.json")
	}
	return outDir, marker, inputPath
}

// moduleRoot returns the framework module root (the directory containing
// orjanda-codegen.mjs) by locating this source file in the module tree. When
// the framework is consumed through Go modules the root resolves inside the
// module cache, so Applications never need a copy of the script.
func moduleRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Dir(filepath.Dir(file))
}

// DefaultScriptPath locates the @orjanda/codegen Node script: an
// orjanda-codegen.mjs in the working directory when present (a vendored or
// workspace copy), else the one shipped in the framework module. When neither
// exists it falls back to the relative name so the exec error names the binary.
func DefaultScriptPath() string {
	if _, err := os.Stat("orjanda-codegen.mjs"); err == nil {
		if abs, err := filepath.Abs("orjanda-codegen.mjs"); err == nil {
			return abs
		}
	}
	if root := moduleRoot(); root != "" {
		p := filepath.Join(root, "orjanda-codegen.mjs")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "orjanda-codegen.mjs"
}

// MarshalInput renders the TAD §6.3 step-1 payload exactly as Regenerate writes
// it (documents sorted by Name, two-space indentation) — the byte shape the
// committed orjanda-ui/src/generated/schema.json must match.
func MarshalInput(reg schema.Registry) ([]byte, error) {
	input, err := CodegenInput(reg)
	if err != nil {
		return nil, err
	}
	sort.Slice(input, func(i, j int) bool { return input[i].Name < input[j].Name })
	return json.MarshalIndent(input, "", "  ")
}

// VerifyCommittedSchema is the node-free half of the generated-output
// consistency check (REVIEW-2026-08-12 finding 5): the on-disk step-1 payload
// must be byte-identical to a fresh CodegenInput for reg. Production
// `orjanda serve` fails fast on a mismatch so a stale TypeScript client cannot
// ship unnoticed; the UI test suite uses it as the commit-time gate on the
// checked-in orjanda-ui/src/generated/schema.json.
func VerifyCommittedSchema(reg schema.Registry, inputPath string) error {
	if inputPath == "" {
		inputPath = filepath.Join("orjanda-ui", "src", "generated", "schema.json")
	}
	committed, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("ui: read committed codegen input: %w", err)
	}
	fresh, err := MarshalInput(reg)
	if err != nil {
		return err
	}
	if !bytes.Equal(committed, fresh) {
		return fmt.Errorf("ui: committed %s is stale — run `npm run codegen` (or `orjanda serve` once) and commit the regenerated output", inputPath)
	}
	return nil
}

// Regenerate runs the codegen pass if and only if the Registry's content hash
// differs from the recorded marker (TAD §6.3 step 3). It returns true when the
// pass ran. On success the marker is updated to the current hash.
func Regenerate(ctx context.Context, opts RegenerateOptions) (bool, error) {
	hash, err := ContentHash(opts.Registry)
	if err != nil {
		return false, err
	}

	outDir, marker, inputPath := defaultPaths(opts)

	if prior, err := os.ReadFile(marker); err == nil && string(prior) == hash {
		return false, nil
	}

	raw, err := MarshalInput(opts.Registry)
	if err != nil {
		return false, err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return false, fmt.Errorf("ui: codegen output dir: %w", err)
	}
	if err := os.WriteFile(inputPath, raw, 0o644); err != nil {
		return false, fmt.Errorf("ui: write codegen input: %w", err)
	}

	script := opts.ScriptPath
	if script == "" {
		script = DefaultScriptPath()
	}
	node := opts.NodeBin
	if node == "" {
		node = "node"
	}

	cmd := exec.CommandContext(ctx, node, script, "--input", inputPath, "--out", outDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("ui: codegen script failed: %w", err)
	}

	if err := os.WriteFile(marker, []byte(hash), 0o644); err != nil {
		return false, fmt.Errorf("ui: write codegen marker: %w", err)
	}
	return true, nil
}
