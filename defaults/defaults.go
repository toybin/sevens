package defaults

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FS holds versioned default assets that ship with sevens.
//
//go:embed functions/* templates/* types/* value-models/*
var FS embed.FS

func ReadFunctionFile(name string) ([]byte, error) {
	return FS.ReadFile(filepath.ToSlash(filepath.Join("functions", name)))
}

// SeedFunctions copies all bundled function files into dir, skipping existing files.
func SeedFunctions(dir string) (int, error) {
	entries, err := FS.ReadDir("functions")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dest := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		data, err := FS.ReadFile("functions/" + e.Name())
		if err != nil {
			return count, err
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func ListFunctionNames() ([]string, error) {
	entries, err := FS.ReadDir("functions")
	if err != nil {
		return nil, fmt.Errorf("read bundled functions: %w", err)
	}
	seen := make(map[string]bool)
	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".edn")
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// SeedTypes copies all bundled type files into dir, skipping existing files.
func SeedTypes(dir string) (int, error) {
	entries, err := FS.ReadDir("types")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dest := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		data, err := FS.ReadFile("types/" + e.Name())
		if err != nil {
			return count, err
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// ListTypeNames returns bundled type definition names.
func ListTypeNames() ([]string, error) {
	entries, err := FS.ReadDir("types")
	if err != nil {
		return nil, fmt.Errorf("read bundled types: %w", err)
	}
	seen := make(map[string]bool)
	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".edn")
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// SeedValueModels copies all bundled value model files into dir, skipping existing files.
func SeedValueModels(dir string) (int, error) {
	entries, err := FS.ReadDir("value-models")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dest := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		data, err := FS.ReadFile("value-models/" + e.Name())
		if err != nil {
			return count, err
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// ListValueModelNames returns bundled value model names.
func ListValueModelNames() ([]string, error) {
	entries, err := FS.ReadDir("value-models")
	if err != nil {
		return nil, fmt.Errorf("read bundled value-models: %w", err)
	}
	seen := make(map[string]bool)
	var names []string
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
		seen[vmName] = true
		names = append(names, vmName)
	}
	sort.Strings(names)
	return names, nil
}

// ReadTemplateFile reads a bundled template file.
// Templates have been migrated to deterministic functions, but
// the old template EDN files are kept for backward compatibility
// with the apply package.
func ReadTemplateFile(name string) ([]byte, error) {
	return FS.ReadFile(filepath.ToSlash(filepath.Join("templates", name)))
}

// ListTemplateNames returns bundled template names.
// Kept for backward compatibility with the apply package.
func ListTemplateNames() ([]string, error) {
	entries, err := FS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("read bundled templates: %w", err)
	}
	seen := make(map[string]bool)
	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".edn")
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
