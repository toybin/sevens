package store

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

// Triple represents a single fact: subject has predicate with value object.
type Triple struct {
	Subject   string
	Predicate string
	Object    string
}

// NodeSubject returns the internal subject identifier for a node title within a root.
// Subjects are opaque and root-qualified so duplicate titles can safely exist across roots.
func NodeSubject(root, title string) string {
	sum := sha1.Sum([]byte(strings.ToLower(root)))
	return fmt.Sprintf("node:%s:%s", hex.EncodeToString(sum[:6]), title)
}

// BlockSubject returns the internal subject identifier for a block within a node/root.
// The path is the block's AST-position path within the markdown document.
func BlockSubject(root, nodeTitle, path string) string {
	sum := sha1.Sum([]byte(strings.ToLower(root)))
	return fmt.Sprintf("block:%s:%s:%s", hex.EncodeToString(sum[:6]), nodeTitle, path)
}

// NodeTitle returns the human-readable title for a node subject.
// Legacy rows that predate node/title store the title in the subject itself.
func NodeTitle(db *sql.DB, subject string) (string, error) {
	title, err := GetObject(db, subject, "node/title")
	if err != nil {
		return "", err
	}
	if title != "" {
		return title, nil
	}
	return subject, nil
}

// scanStrings scans a single-column string result set into a slice.
func scanStrings(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var results []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

// scanTriples scans a (subject, predicate, object) result set into a slice.
func scanTriples(rows *sql.Rows) ([]Triple, error) {
	defer rows.Close()
	var results []Triple
	for rows.Next() {
		var t Triple
		if err := rows.Scan(&t.Subject, &t.Predicate, &t.Object); err != nil {
			return nil, err
		}
		results = append(results, t)
	}
	return results, rows.Err()
}

// InitTriplesSchema creates the triples table and indexes.
func InitTriplesSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS triples (
			subject TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object TEXT NOT NULL,
			PRIMARY KEY (subject, predicate, object)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_triples_pred_obj ON triples (predicate, object)`,
		`CREATE INDEX IF NOT EXISTS idx_triples_obj ON triples (object)`,
		`CREATE INDEX IF NOT EXISTS idx_triples_pred ON triples (predicate)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("init triples schema: %w", err)
		}
	}
	return nil
}

// InsertTriple inserts a single triple. Idempotent for the exact same
// (subject, predicate, object) triple, but does NOT replace an existing triple
// that shares only (subject, predicate). Use SetTriple for singular predicates.
func InsertTriple(db *sql.DB, t Triple) error {
	_, err := db.Exec(
		`INSERT OR REPLACE INTO triples (subject, predicate, object) VALUES (?, ?, ?)`,
		t.Subject, t.Predicate, t.Object,
	)
	return err
}

// SetTriple sets a singular predicate value, replacing any existing value.
// Use this for predicates that should have exactly one value per subject.
// For multi-valued predicates, use InsertTriple instead.
func SetTriple(db *sql.DB, subject, predicate, object string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec("DELETE FROM triples WHERE subject = ? AND predicate = ?", subject, predicate)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO triples (subject, predicate, object) VALUES (?, ?, ?)", subject, predicate, object)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// InsertTriples inserts multiple triples in a single transaction.
func InsertTriples(db *sql.DB, triples []Triple) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO triples (subject, predicate, object) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, t := range triples {
		if _, err := stmt.Exec(t.Subject, t.Predicate, t.Object); err != nil {
			return fmt.Errorf("insert triple (%s, %s, %s): %w", t.Subject, t.Predicate, t.Object, err)
		}
	}

	return tx.Commit()
}

// DeleteBySubject removes all triples for a given subject.
func DeleteBySubject(db *sql.DB, subject string) error {
	_, err := db.Exec(`DELETE FROM triples WHERE subject = ?`, subject)
	return err
}

// DeleteBySubjectPrefix removes all triples where subject starts with prefix.
func DeleteBySubjectPrefix(db *sql.DB, prefix string) error {
	_, err := db.Exec(`DELETE FROM triples WHERE subject LIKE ?`, prefix+"%")
	return err
}

// DeleteByPredicate removes all triples with a given predicate.
func DeleteByPredicate(db *sql.DB, predicate string) error {
	_, err := db.Exec(`DELETE FROM triples WHERE predicate = ?`, predicate)
	return err
}

// ClearRootTriples removes all triples associated with a root (node triples, ref triples, content triples, etc.)
func ClearRootTriples(db *sql.DB, root string) error {
	// Find all subjects that belong to this root
	rows, err := db.Query(`
		SELECT subject FROM triples
		WHERE (predicate = 'node/root' OR predicate = 'block/root') AND object = ?
	`, root)
	if err != nil {
		return fmt.Errorf("query root subjects: %w", err)
	}
	defer rows.Close()

	var subjects []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return fmt.Errorf("scan subject: %w", err)
		}
		subjects = append(subjects, s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate subjects: %w", err)
	}

	if len(subjects) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, subj := range subjects {
		if _, err := tx.Exec(`DELETE FROM triples WHERE subject = ?`, subj); err != nil {
			return fmt.Errorf("delete triples for %s: %w", subj, err)
		}
	}

	return tx.Commit()
}

// --- Query helpers ---

// ResolveTitle does a case-insensitive lookup for a node title within a root.
// Returns the canonical (as-stored) title, or "" if not found.
func ResolveTitle(db *sql.DB, title, root string) string {
	_, canonical := ResolveNode(db, title, root)
	return canonical
}

// ResolveNode does a case-insensitive lookup for a node title within a root.
// It returns the internal subject and canonical title, or empty strings if not found.
func ResolveNode(db *sql.DB, title, root string) (string, string) {
	var subject, canonical string
	err := db.QueryRow(`
		SELECT nr.subject, COALESCE(nt.object, nr.subject) AS title
		FROM triples nr
		LEFT JOIN triples nt ON nr.subject = nt.subject AND nt.predicate = 'node/title'
		WHERE nr.predicate = 'node/root' AND nr.object = ?
		AND (
			(nt.object IS NOT NULL AND nt.object = ? COLLATE NOCASE)
			OR (nt.object IS NULL AND nr.subject = ? COLLATE NOCASE)
		)
		LIMIT 1
	`, root, title, title).Scan(&subject, &canonical)
	if err != nil {
		return "", ""
	}
	return subject, canonical
}

// GetObject returns the single object for a (subject, predicate) pair, or "" if not found.
// Use for unique predicates like node/content, node/parent, etc.
func GetObject(db *sql.DB, subject, predicate string) (string, error) {
	var obj string
	err := db.QueryRow(
		`SELECT object FROM triples WHERE subject = ? AND predicate = ?`,
		subject, predicate,
	).Scan(&obj)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return obj, err
}

// GetObjects returns all objects for a (subject, predicate) pair.
// Use for multi-valued predicates like ref/wiki-link, context/file, etc.
func GetObjects(db *sql.DB, subject, predicate string) ([]string, error) {
	rows, err := db.Query(
		`SELECT object FROM triples WHERE subject = ? AND predicate = ?`,
		subject, predicate,
	)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// GetSubjects returns all subjects that have a given (predicate, object) pair.
// This is the inverse/reverse lookup — e.g., find all children of a parent.
func GetSubjects(db *sql.DB, predicate, object string) ([]string, error) {
	rows, err := db.Query(
		`SELECT subject FROM triples WHERE predicate = ? AND object = ?`,
		predicate, object,
	)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// GetSubjectTriples returns all triples for a given subject.
func GetSubjectTriples(db *sql.DB, subject string) ([]Triple, error) {
	rows, err := db.Query(
		`SELECT subject, predicate, object FROM triples WHERE subject = ?`,
		subject,
	)
	if err != nil {
		return nil, err
	}
	return scanTriples(rows)
}

// GetPredicateTriples returns all triples with a given predicate.
func GetPredicateTriples(db *sql.DB, predicate string) ([]Triple, error) {
	rows, err := db.Query(
		`SELECT subject, predicate, object FROM triples WHERE predicate = ?`,
		predicate,
	)
	if err != nil {
		return nil, err
	}
	return scanTriples(rows)
}

// SearchContent finds subjects whose node/content contains the query string.
func SearchContent(db *sql.DB, query string, root string) ([]string, error) {
	rows, err := db.Query(
		`SELECT DISTINCT COALESCE(t3.object, t1.subject) FROM triples t1
		 JOIN triples t2 ON t1.subject = t2.subject
		 LEFT JOIN triples t3 ON t1.subject = t3.subject AND t3.predicate = 'node/title'
		 WHERE t1.predicate = 'node/content' AND t1.object LIKE ?
		 AND t2.predicate = 'node/root' AND t2.object = ?`,
		"%"+query+"%", root,
	)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// SearchTitles finds node titles matching the query string within a root.
func SearchTitles(db *sql.DB, query string, root string) ([]string, error) {
	rows, err := db.Query(
		`SELECT DISTINCT COALESCE(nt.object, nr.subject) FROM triples nr
		 LEFT JOIN triples nt ON nr.subject = nt.subject AND nt.predicate = 'node/title'
		 WHERE nr.predicate = 'node/root'
		 AND nr.object = ?
		 AND COALESCE(nt.object, nr.subject) LIKE ?`,
		root, "%"+query+"%",
	)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// ListNodeTitles returns all node titles for a root.
func ListNodeTitles(db *sql.DB, root string) ([]string, error) {
	rows, err := db.Query(`
		SELECT COALESCE(nt.object, nr.subject) FROM triples nr
		LEFT JOIN triples nt ON nr.subject = nt.subject AND nt.predicate = 'node/title'
		WHERE nr.predicate = 'node/root' AND nr.object = ?
	`, root)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// GetRootNodeData fetches triples for nodes in a root keyed by node title.
// Returns a map of title → predicate → []object. This is the batch alternative to
// calling GetObject/GetObjects in a loop (N+1 query problem).
func GetRootNodeData(db *sql.DB, root string, predicates []string) (map[string]map[string][]string, error) {
	if len(predicates) == 0 {
		return nil, nil
	}
	// Build placeholders for predicates
	placeholders := make([]string, len(predicates))
	args := make([]any, 0, len(predicates)+1)
	args = append(args, root)
	for i, p := range predicates {
		placeholders[i] = "?"
		args = append(args, p)
	}

	query := fmt.Sprintf(`
		SELECT COALESCE(tt.object, t1.subject), t1.predicate, t1.object FROM triples t1
		JOIN triples t2 ON t1.subject = t2.subject
		LEFT JOIN triples tt ON t1.subject = tt.subject AND tt.predicate = 'node/title'
		WHERE t2.predicate = 'node/root' AND t2.object = ?
		AND t1.predicate IN (%s)
		ORDER BY 1, t1.predicate
	`, fmt.Sprintf("%s", joinPlaceholders(placeholders)))

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]map[string][]string)
	for rows.Next() {
		var subj, pred, obj string
		if err := rows.Scan(&subj, &pred, &obj); err != nil {
			return nil, err
		}
		if result[subj] == nil {
			result[subj] = make(map[string][]string)
		}
		result[subj][pred] = append(result[subj][pred], obj)
	}
	return result, rows.Err()
}

func joinPlaceholders(ph []string) string {
	result := ""
	for i, p := range ph {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}

// RunQuery executes a raw SQL query (for PRQL-compiled queries) and returns results as string slices.
func RunQuery(db *sql.DB, sqlQuery string, args ...any) ([][]string, error) {
	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("getting columns: %w", err)
	}

	var results [][]string
	// Add header row
	results = append(results, cols)

	for rows.Next() {
		values := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		row := make([]string, len(cols))
		for i, v := range values {
			if v.Valid {
				row[i] = v.String
			}
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

// Compose performs a two-step morphism composition:
// Starting from `start`, follow `pred1` to intermediate objects,
// then follow `pred2` from those intermediates to final objects.
// Returns the final objects.
func Compose(db *sql.DB, start, pred1, pred2 string) ([]string, error) {
	rows, err := db.Query(`
		SELECT t2.object FROM triples t1
		JOIN triples t2 ON t1.object = t2.subject
		WHERE t1.subject = ? AND t1.predicate = ? AND t2.predicate = ?
	`, start, pred1, pred2)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// ComposeInverse performs: start →pred1→ intermediate ←pred2← results
// i.e., follow pred1 forward, then pred2 in reverse (find subjects that point to intermediate via pred2).
func ComposeInverse(db *sql.DB, start, pred1, pred2 string) ([]string, error) {
	rows, err := db.Query(`
		SELECT t2.subject FROM triples t1
		JOIN triples t2 ON t1.object = t2.object
		WHERE t1.subject = ? AND t1.predicate = ? AND t2.predicate = ?
		AND t2.subject != ?
	`, start, pred1, pred2, start)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}
