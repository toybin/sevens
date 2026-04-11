package defaults

import (
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// FS holds versioned default assets that ship with sevens.
//
//go:embed functions/* templates/*
var FS embed.FS

func ReadFunctionFile(name string) ([]byte, error) {
	return FS.ReadFile(filepath.ToSlash(filepath.Join("functions", name)))
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

func ReadTemplateFile(name string) ([]byte, error) {
	return FS.ReadFile(filepath.ToSlash(filepath.Join("templates", name)))
}

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
