// Package triple is Layer 1: the bare triple store.
//
// Stores and retrieves (subject, predicate, object) string triples
// in a SQLite table. No domain knowledge -- all semantic meaning
// is imposed by packages above.
package triple

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Triple is a single fact: (subject, predicate, object).
type Triple struct {
	Subject   string
	Predicate string
	Object    string
}

// Store wraps a SQL database and provides CRUD operations on triples.
type Store struct {
	db *sql.DB
}

// New creates a Store backed by the given database and initializes
// the schema if needed.
func New(db *sql.DB) (*Store, error) {
	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("triple: init schema: %w", err)
	}
	return &Store{db: db}, nil
}

// DB returns the underlying database handle. Escape hatch for
// callers that need raw SQL access.
func (s *Store) DB() *sql.DB { return s.db }

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS triples (
			subject   TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object    TEXT NOT NULL,
			UNIQUE(subject, predicate, object)
		);
		CREATE INDEX IF NOT EXISTS idx_sp ON triples(subject, predicate);
		CREATE INDEX IF NOT EXISTS idx_po ON triples(predicate, object);
		CREATE INDEX IF NOT EXISTS idx_spo ON triples(subject, predicate, object);
	`)
	return err
}

// Assert adds a triple if not already present. Idempotent.
func (s *Store) Assert(ctx context.Context, t Triple) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO triples (subject, predicate, object) VALUES (?, ?, ?)`,
		t.Subject, t.Predicate, t.Object)
	return err
}

// AssertBatch adds multiple triples in a single transaction.
func (s *Store) AssertBatch(ctx context.Context, triples []Triple) error {
	if len(triples) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO triples (subject, predicate, object) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range triples {
		if _, err := stmt.ExecContext(ctx, t.Subject, t.Predicate, t.Object); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Retract removes a specific triple. No error if it doesn't exist.
func (s *Store) Retract(ctx context.Context, t Triple) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM triples WHERE subject = ? AND predicate = ? AND object = ?`,
		t.Subject, t.Predicate, t.Object)
	return err
}

// RetractBySubject removes all triples with the given subject.
func (s *Store) RetractBySubject(ctx context.Context, subject string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM triples WHERE subject = ?`, subject)
	return err
}

// RetractBySubjectPrefix removes all triples whose subject starts with prefix.
func (s *Store) RetractBySubjectPrefix(ctx context.Context, prefix string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM triples WHERE subject LIKE ?`, prefix+"%")
	return err
}

// RetractByPredicate removes all triples with the given predicate.
func (s *Store) RetractByPredicate(ctx context.Context, predicate string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM triples WHERE predicate = ?`, predicate)
	return err
}

// --- Queries ---

// BySubject returns all triples for a subject.
func (s *Store) BySubject(ctx context.Context, subject string) ([]Triple, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT subject, predicate, object FROM triples WHERE subject = ?`, subject)
	if err != nil {
		return nil, err
	}
	return scanTriples(rows)
}

// BySubjectPredicate returns all objects for a (subject, predicate) pair.
func (s *Store) BySubjectPredicate(ctx context.Context, subject, predicate string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT object FROM triples WHERE subject = ? AND predicate = ?`,
		subject, predicate)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// ByPredicateObject returns all subjects for a (predicate, object) pair.
func (s *Store) ByPredicateObject(ctx context.Context, predicate, object string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT subject FROM triples WHERE predicate = ? AND object = ?`,
		predicate, object)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// ByPredicate returns all triples with a given predicate.
func (s *Store) ByPredicate(ctx context.Context, predicate string) ([]Triple, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT subject, predicate, object FROM triples WHERE predicate = ?`, predicate)
	if err != nil {
		return nil, err
	}
	return scanTriples(rows)
}

// Search finds subjects whose object for the given predicate contains
// the substring. Case-insensitive.
func (s *Store) Search(ctx context.Context, predicate, substring string) ([]string, error) {
	pattern := "%" + strings.ReplaceAll(substring, "%", "%%") + "%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT subject FROM triples WHERE predicate = ? AND object LIKE ? COLLATE NOCASE`,
		predicate, pattern)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// RawQuery executes arbitrary SQL and returns results as string rows.
// First row is column headers.
func (s *Store) RawQuery(ctx context.Context, query string, args ...any) ([][]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := [][]string{cols}

	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			if v.Valid {
				row[i] = v.String
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// --- Helpers ---

func scanTriples(rows *sql.Rows) ([]Triple, error) {
	defer rows.Close()
	var result []Triple
	for rows.Next() {
		var t Triple
		if err := rows.Scan(&t.Subject, &t.Predicate, &t.Object); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func scanStrings(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var result []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
