package kb

import (
	"context"
	"crypto/rand"
	"fmt"
	"sort"
	"time"

	"sevens/internal/triple"
)

// LogEntry records a function application or pipeline transition.
type LogEntry struct {
	Subject      string
	Event        string
	Root         string
	Function     string
	Node         string
	Step         string
	StepIndex    string
	Timestamp    string
	Session      string
	Result       string
	Commit       string
	Note         string
	FilesCreated []string
	FilesEdited  []string
}

// AppendLog writes a log entry as triples. Generates a unique subject
// for the entry.
func (k *KB) AppendLog(ctx context.Context, entry LogEntry) error {
	if entry.Subject == "" {
		entry.Subject = logSubject(entry.Node)
	}

	triples := []triple.Triple{
		{Subject: entry.Subject, Predicate: PredLogEvent, Object: entry.Event},
		{Subject: entry.Subject, Predicate: PredLogTimestamp, Object: entry.Timestamp},
	}
	if entry.Root != "" {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogRoot, Object: entry.Root})
	}
	if entry.Function != "" {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogFunction, Object: entry.Function})
	}
	if entry.Node != "" {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogNode, Object: entry.Node})
	}
	if entry.Step != "" {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogStep, Object: entry.Step})
	}
	if entry.StepIndex != "" {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogStepIndex, Object: entry.StepIndex})
	}
	if entry.Session != "" {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogSession, Object: entry.Session})
	}
	if entry.Result != "" {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogResult, Object: entry.Result})
	}
	if entry.Commit != "" {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogCommit, Object: entry.Commit})
	}
	if entry.Note != "" {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogNote, Object: entry.Note})
	}
	for _, f := range entry.FilesCreated {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogFilesCreated, Object: f})
	}
	for _, f := range entry.FilesEdited {
		triples = append(triples, triple.Triple{Subject: entry.Subject, Predicate: PredLogFilesEdited, Object: f})
	}

	return k.graph.Store().AssertBatch(ctx, triples)
}

// ReadLog returns log entries for a node, ordered by timestamp.
func (k *KB) ReadLog(ctx context.Context, root, nodeTitle string) ([]LogEntry, error) {
	// Find all log subjects that reference this node
	subjects, err := k.graph.Store().ByPredicateObject(ctx, PredLogNode, nodeTitle)
	if err != nil {
		return nil, err
	}

	var entries []LogEntry
	for _, subj := range subjects {
		// Optionally filter by root
		if root != "" {
			r, _, _ := k.graph.Lookup(ctx, subj, PredLogRoot)
			if r != root {
				continue
			}
		}

		entry := LogEntry{Subject: subj}
		entry.Event, _, _ = k.graph.Lookup(ctx, subj, PredLogEvent)
		entry.Root, _, _ = k.graph.Lookup(ctx, subj, PredLogRoot)
		entry.Function, _, _ = k.graph.Lookup(ctx, subj, PredLogFunction)
		entry.Node, _, _ = k.graph.Lookup(ctx, subj, PredLogNode)
		entry.Step, _, _ = k.graph.Lookup(ctx, subj, PredLogStep)
		entry.StepIndex, _, _ = k.graph.Lookup(ctx, subj, PredLogStepIndex)
		entry.Timestamp, _, _ = k.graph.Lookup(ctx, subj, PredLogTimestamp)
		entry.Session, _, _ = k.graph.Lookup(ctx, subj, PredLogSession)
		entry.Result, _, _ = k.graph.Lookup(ctx, subj, PredLogResult)
		entry.Commit, _, _ = k.graph.Lookup(ctx, subj, PredLogCommit)
		entry.Note, _, _ = k.graph.Lookup(ctx, subj, PredLogNote)
		entry.FilesCreated, _ = k.graph.Store().BySubjectPredicate(ctx, subj, PredLogFilesCreated)
		entry.FilesEdited, _ = k.graph.Store().BySubjectPredicate(ctx, subj, PredLogFilesEdited)
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})
	return entries, nil
}

func logSubject(nodeTitle string) string {
	ts := time.Now().UTC().Format("20060102T150405")
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("log:%s:%s:%x", ts, nodeTitle, b)
}
