package apply

import (
	"fmt"
	"os"
	"path/filepath"

	"olympos.io/encoding/edn"
	"sevens/internal/store"
)

// Session represents a focused node session.
type Session struct {
	Root      string   `edn:"root"`
	NodeTitle string   `edn:"node-title"`
	CreatedAt string   `edn:"created-at"`
	Includes  []string `edn:"includes"`  // additional node titles in context
	Excludes  []string `edn:"excludes"`  // explicitly excluded nodes
}

func SessionPath() (string, error) {
	dir, err := store.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.edn"), nil
}

func SaveSession(s *Session) error {
	path, err := SessionPath()
	if err != nil {
		return err
	}
	data, err := edn.MarshalPPrint(s, nil)
	if err != nil {
		return fmt.Errorf("marshalling session: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func LoadSession() (*Session, error) {
	path, err := SessionPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}
	var s Session
	if err := edn.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing session: %w", err)
	}
	return &s, nil
}

func ClearSession() error {
	path, err := SessionPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
