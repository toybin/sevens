package apply

import (
	crypto_rand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"sevens/internal/store"
)

// logSubject generates a unique subject for a log entry.
// A 4-byte random suffix is appended to prevent collisions when multiple
// events are written for the same node within the same second.
func logSubject(nodeTitle, timestamp string) string {
	sanitized := strings.ToLower(nodeTitle)
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	re := regexp.MustCompile(`[^a-z0-9-]+`)
	sanitized = re.ReplaceAllString(sanitized, "")
	buf := make([]byte, 4)
	crypto_rand.Read(buf)
	suffix := hex.EncodeToString(buf)
	return fmt.Sprintf("log:%s:%s:%s", timestamp, sanitized, suffix)
}

// AppendLogDB writes a log entry as triples to the database.
func AppendLogDB(db *sql.DB, entry LogEntry) error {
	subject := logSubject(entry.Target, entry.Timestamp)

	var triples []store.Triple
	triples = append(triples, store.Triple{Subject: subject, Predicate: "log/event", Object: entry.Event})
	if entry.Root != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/root", Object: entry.Root})
	}
	triples = append(triples, store.Triple{Subject: subject, Predicate: "log/target", Object: entry.Target})
	triples = append(triples, store.Triple{Subject: subject, Predicate: "log/timestamp", Object: entry.Timestamp})

	if entry.Function != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/function", Object: entry.Function})
	}
	if entry.Step != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/step", Object: entry.Step})
	}
	if entry.StepIndex > 0 {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/step-index", Object: strconv.Itoa(entry.StepIndex)})
	}
	if entry.RawOutput != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/raw-output", Object: entry.RawOutput})
	}
	if entry.Summary != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/summary", Object: entry.Summary})
	}
	if entry.Commit != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/commit", Object: entry.Commit})
	}
	if entry.Note != "" {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/note", Object: entry.Note})
	}
	for _, f := range entry.FilesCreated {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/file-created", Object: f})
	}
	for _, f := range entry.FilesEdited {
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/file-edited", Object: f})
	}
	if len(entry.Ops) > 0 {
		opsJSON, _ := json.Marshal(entry.Ops)
		triples = append(triples, store.Triple{Subject: subject, Predicate: "log/ops", Object: string(opsJSON)})
	}

	return store.InsertTriples(db, triples)
}

// ReadLogDB reads log entries for a node, ordered by timestamp.
// Preferred call form is ReadLogDB(db, root, nodeTitle). The legacy
// ReadLogDB(db, nodeTitle) form is still accepted for tests and old data.
func ReadLogDB(db *sql.DB, parts ...string) ([]LogEntry, error) {
	var root, nodeTitle string
	switch len(parts) {
	case 1:
		nodeTitle = parts[0]
	case 2:
		root = parts[0]
		nodeTitle = parts[1]
	default:
		return nil, fmt.Errorf("ReadLogDB expects nodeTitle or root,nodeTitle")
	}

	var (
		rows *sql.Rows
		err  error
	)
	if root != "" {
		rows, err = db.Query(`
			SELECT DISTINCT t1.subject FROM triples t1
			JOIN triples t2 ON t1.subject = t2.subject
			WHERE t1.predicate = 'log/target' AND t1.object = ?
			AND t2.predicate = 'log/root' AND t2.object = ?
		`, nodeTitle, root)
	} else {
		rows, err = db.Query(`
			SELECT DISTINCT subject FROM triples
			WHERE predicate = 'log/target' AND object = ?
		`, nodeTitle)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subjects []string
	for rows.Next() {
		var subj string
		if err := rows.Scan(&subj); err != nil {
			return nil, err
		}
		subjects = append(subjects, subj)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(subjects) // subjects contain timestamps, so sorting works

	var entries []LogEntry
	for _, subj := range subjects {
		entry, err := logSubjectToEntry(db, subj)
		if err != nil {
			continue
		}
		entries = append(entries, *entry)
	}
	return entries, nil
}

// logSubjectToEntry reconstructs a LogEntry from its triples.
func logSubjectToEntry(db *sql.DB, subject string) (*LogEntry, error) {
	triples, err := store.GetSubjectTriples(db, subject)
	if err != nil {
		return nil, err
	}

	entry := &LogEntry{}
	for _, t := range triples {
		switch t.Predicate {
		case "log/event":
			entry.Event = t.Object
		case "log/function":
			entry.Function = t.Object
		case "log/root":
			entry.Root = t.Object
		case "log/target":
			entry.Target = t.Object
		case "log/step":
			entry.Step = t.Object
		case "log/step-index":
			entry.StepIndex, _ = strconv.Atoi(t.Object)
		case "log/timestamp":
			entry.Timestamp = t.Object
		case "log/raw-output":
			entry.RawOutput = t.Object
		case "log/summary":
			entry.Summary = t.Object
		case "log/commit":
			entry.Commit = t.Object
		case "log/note":
			entry.Note = t.Object
		case "log/file-created":
			entry.FilesCreated = append(entry.FilesCreated, t.Object)
		case "log/file-edited":
			entry.FilesEdited = append(entry.FilesEdited, t.Object)
		case "log/ops":
			json.Unmarshal([]byte(t.Object), &entry.Ops)
		}
	}
	return entry, nil
}
