package types

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"olympos.io/encoding/edn"
	"sevens/internal/config"
)

// ValueModelKind identifies the class of value model.
type ValueModelKind string

const (
	VMEnum         ValueModelKind = "enum"
	VMStateMachine ValueModelKind = "state-machine"
	VMDate         ValueModelKind = "date"
	VMReference    ValueModelKind = "reference"
	VMString       ValueModelKind = "string"
)

// ValueModel defines how predicate values are validated and normalized.
type ValueModel struct {
	Name        string
	Kind        ValueModelKind
	Members     []string          // for enum/state-machine: valid values
	Transitions [][2]string       // for state-machine: [from, to] pairs
	Aliases     map[string]string // display alias -> canonical value
	Format      string            // for date: Go format string
	ResolvesTo  string            // for reference: "node" or "external"
	Signifier   string            // for reference: "@" etc.
}

// Validate checks if a value is valid for this model.
// For state machines, this performs static validation only (is the value
// a valid state?). Transition validation requires the current state and
// is handled separately.
func (vm *ValueModel) Validate(value string) error {
	switch vm.Kind {
	case VMEnum, VMStateMachine:
		// Check direct membership.
		for _, m := range vm.Members {
			if m == value {
				return nil
			}
		}
		// Check aliases.
		if _, ok := vm.Aliases[value]; ok {
			return nil
		}
		return fmt.Errorf("value %q is not a valid member of %s %q (valid: %v)",
			value, vm.Kind, vm.Name, vm.Members)

	case VMDate:
		format := vm.Format
		if format == "" {
			format = "2006-01-02"
		}
		if _, err := time.Parse(format, value); err != nil {
			return fmt.Errorf("value %q does not match date format %q: %w",
				value, format, err)
		}
		return nil

	case VMReference:
		if value == "" {
			return fmt.Errorf("reference value must be non-empty")
		}
		return nil

	case VMString:
		return nil

	default:
		return fmt.Errorf("unknown value model kind %q", vm.Kind)
	}
}

// Resolve returns the canonical value for an input, resolving aliases.
// If the value is not an alias, it is returned unchanged.
func (vm *ValueModel) Resolve(value string) string {
	if canonical, ok := vm.Aliases[value]; ok {
		return canonical
	}
	return value
}

// ValidateTransition checks if a state machine transition is valid.
// Returns nil if the transition is allowed, or an error if not.
// Only meaningful for state-machine value models.
func (vm *ValueModel) ValidateTransition(from, to string) error {
	if vm.Kind != VMStateMachine {
		return fmt.Errorf("transitions only apply to state-machine value models, got %q", vm.Kind)
	}
	for _, t := range vm.Transitions {
		if t[0] == from && t[1] == to {
			return nil
		}
	}
	return fmt.Errorf("transition %q -> %q is not allowed in %q", from, to, vm.Name)
}

// --- EDN loading ---

// ednValueModel is the EDN wire format for value model definitions.
type ednValueModel struct {
	Name        string            `edn:"name"`
	Kind        string            `edn:"kind"`
	Members     []string          `edn:"members"`
	States      []string          `edn:"states"`
	Transitions [][2]string       `edn:"transitions"`
	Aliases     map[string]string `edn:"aliases"`
	Format      string            `edn:"format"`
	ResolvesTo  string            `edn:"resolves-to"`
	Signifier   string            `edn:"signifier"`
}

// valueModelsDir returns the path to the value-models configuration directory.
// Value models can live in ~/.config/sevens/value-models/ or alongside
// type defs in ~/.config/sevens/types/ with a .vm.edn extension.
func valueModelsDir() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	vmDir := filepath.Join(dir, "value-models")
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		return "", fmt.Errorf("create value-models dir: %w", err)
	}
	return vmDir, nil
}

// LoadValueModel loads a single value model by name.
// Searches value-models/ dir first, then types/ dir for .vm.edn files.
func LoadValueModel(name string) (*ValueModel, error) {
	// Try value-models/ directory.
	vmDir, err := valueModelsDir()
	if err == nil {
		path := filepath.Join(vmDir, name+".vm.edn")
		if data, err := os.ReadFile(path); err == nil {
			return parseValueModel(data, name)
		}
	}

	// Try types/ directory with .vm.edn extension.
	tDir, err := typesDir()
	if err == nil {
		path := filepath.Join(tDir, name+".vm.edn")
		if data, err := os.ReadFile(path); err == nil {
			return parseValueModel(data, name)
		}
	}

	return nil, fmt.Errorf("value model %q not found", name)
}

// ListValueModels returns all defined value models.
func ListValueModels() ([]ValueModel, error) {
	seen := make(map[string]bool)
	var models []ValueModel

	// Scan value-models/ directory.
	vmDir, err := valueModelsDir()
	if err == nil {
		models, seen = scanVMDir(vmDir, models, seen)
	}

	// Scan types/ directory for .vm.edn files.
	tDir, err := typesDir()
	if err == nil {
		models, _ = scanVMDir(tDir, models, seen)
	}

	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	return models, nil
}

// scanVMDir scans a directory for .vm.edn files and appends parsed models.
func scanVMDir(dir string, models []ValueModel, seen map[string]bool) ([]ValueModel, map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return models, seen
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".vm.edn") {
			continue
		}
		vmName := strings.TrimSuffix(name, ".vm.edn")
		if seen[vmName] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		vm, err := parseValueModel(data, vmName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			continue
		}
		seen[vmName] = true
		models = append(models, *vm)
	}
	return models, seen
}

// parseValueModel parses EDN bytes into a ValueModel.
func parseValueModel(data []byte, filename string) (*ValueModel, error) {
	var raw ednValueModel
	if err := edn.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse value model %s: %w", filename, err)
	}
	if raw.Name == "" {
		return nil, fmt.Errorf("value model %s: name must be non-empty", filename)
	}

	kind := ValueModelKind(raw.Kind)
	switch kind {
	case VMEnum, VMStateMachine, VMDate, VMReference, VMString:
		// valid
	default:
		return nil, fmt.Errorf("value model %s: unknown kind %q", filename, raw.Kind)
	}

	vm := &ValueModel{
		Name:        raw.Name,
		Kind:        kind,
		Aliases:     raw.Aliases,
		Format:      raw.Format,
		ResolvesTo:  raw.ResolvesTo,
		Signifier:   raw.Signifier,
		Transitions: raw.Transitions,
	}

	// State machines use "states" field; enums use "members".
	switch kind {
	case VMStateMachine:
		vm.Members = raw.States
		if len(vm.Members) == 0 {
			vm.Members = raw.Members // fallback
		}
	default:
		vm.Members = raw.Members
	}

	return vm, nil
}
