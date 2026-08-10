package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	// Defaults to the orjanda-codegen.mjs beside the repo root.
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

// Regenerate runs the codegen pass if and only if the Registry's content hash
// differs from the recorded marker (TAD §6.3 step 3). It returns true when the
// pass ran. On success the marker is updated to the current hash.
func Regenerate(ctx context.Context, opts RegenerateOptions) (bool, error) {
	hash, err := ContentHash(opts.Registry)
	if err != nil {
		return false, err
	}

	outDir := opts.OutputDir
	if outDir == "" {
		outDir = "orjanda-ui/src/generated"
	}
	marker := opts.MarkerPath
	if marker == "" {
		marker = filepath.Join(outDir, ".registry-hash")
	}
	inputPath := opts.InputPath
	if inputPath == "" {
		inputPath = filepath.Join(outDir, "schema.json")
	}

	if prior, err := os.ReadFile(marker); err == nil && string(prior) == hash {
		return false, nil
	}

	input, err := CodegenInput(opts.Registry)
	if err != nil {
		return false, err
	}
	sort.Slice(input, func(i, j int) bool { return input[i].Name < input[j].Name })
	raw, err := json.MarshalIndent(input, "", "  ")
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
		wd, err := os.Getwd()
		if err != nil {
			return false, err
		}
		script = filepath.Join(wd, "orjanda-codegen.mjs")
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
