package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"olympos.io/encoding/edn"
	_ "turso.tech/database/tursogo"
)

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".config", "sevens")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create config dir %s: %w", dir, err)
	}
	return dir, nil
}

func OpenDB() (*sql.DB, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("turso", filepath.Join(dir, "sevens.db"))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// WAL mode allows concurrent readers; busy_timeout retries on lock contention.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not set WAL mode: %v\n", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not set busy_timeout: %v\n", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}


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
