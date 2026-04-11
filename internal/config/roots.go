package config

import (
	"fmt"
	"os"
	"path/filepath"

	"olympos.io/encoding/edn"
)

// LoadRoots reads the list of registered roots from roots.edn.
func LoadRoots() ([]string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "roots.edn"))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading roots.edn: %w", err)
	}
	var roots []string
	if err := edn.Unmarshal(data, &roots); err != nil {
		return nil, fmt.Errorf("parsing roots.edn: %w", err)
	}
	return roots, nil
}

// SaveRoots writes the list of roots to roots.edn.
func SaveRoots(roots []string) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	data, err := edn.MarshalPPrint(roots, nil)
	if err != nil {
		return fmt.Errorf("marshalling roots: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "roots.edn"), append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing roots.edn: %w", err)
	}
	return nil
}

// AddRoot registers a root directory, deduplicating.
func AddRoot(root string) error {
	roots, err := LoadRoots()
	if err != nil {
		return err
	}
	for _, r := range roots {
		if r == root {
			return nil
		}
	}
	roots = append(roots, root)
	return SaveRoots(roots)
}
