package kb

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"sevens/internal/triple"
)

// Session represents a working context.
type Session struct {
	Subject  string
	Root     string   // root directory path
	Focus    string   // node title (for current session) or subject
	Includes []string // node titles or subjects
	Excludes []string // node titles or subjects
	Started  string
	Ended    string
}

// StartSession creates a new session with a focus node.
func (k *KB) StartSession(ctx context.Context, focusSubject string) (*Session, error) {
	subject := sessionSubject()
	ts := time.Now().UTC().Format(time.RFC3339)

	triples := []triple.Triple{
		{Subject: subject, Predicate: PredSessionFocus, Object: focusSubject},
		{Subject: subject, Predicate: PredSessionStarted, Object: ts},
	}
	if err := k.graph.Store().AssertBatch(ctx, triples); err != nil {
		return nil, err
	}

	return &Session{
		Subject: subject,
		Focus:   focusSubject,
		Started: ts,
	}, nil
}

// SetFocus changes the active focus for a session.
func (k *KB) SetFocus(ctx context.Context, sessionSubject, nodeSubject string) error {
	return k.graph.Set(ctx, sessionSubject, PredSessionFocus, nodeSubject)
}

// AddInclude adds a node to the session's context includes.
func (k *KB) AddInclude(ctx context.Context, sessionSubject, nodeSubject string) error {
	return k.graph.Store().Assert(ctx, triple.Triple{
		Subject: sessionSubject, Predicate: PredSessionInclude, Object: nodeSubject,
	})
}

// RemoveInclude removes a node from the session's context includes.
func (k *KB) RemoveInclude(ctx context.Context, sessionSubject, nodeSubject string) error {
	return k.graph.Store().Retract(ctx, triple.Triple{
		Subject: sessionSubject, Predicate: PredSessionInclude, Object: nodeSubject,
	})
}

// AddExclude adds a node to the session's context excludes.
func (k *KB) AddExclude(ctx context.Context, sessionSubject, nodeSubject string) error {
	return k.graph.Store().Assert(ctx, triple.Triple{
		Subject: sessionSubject, Predicate: PredSessionExclude, Object: nodeSubject,
	})
}

// EndSession marks a session as ended.
func (k *KB) EndSession(ctx context.Context, sessionSubject string) error {
	ts := time.Now().UTC().Format(time.RFC3339)
	return k.graph.Set(ctx, sessionSubject, PredSessionEnded, ts)
}

// LoadSession reconstructs a session from its triples.
func (k *KB) LoadSession(ctx context.Context, sessionSubject string) (*Session, error) {
	s := &Session{Subject: sessionSubject}
	s.Focus, _, _ = k.graph.Lookup(ctx, sessionSubject, PredSessionFocus)
	s.Started, _, _ = k.graph.Lookup(ctx, sessionSubject, PredSessionStarted)
	s.Ended, _, _ = k.graph.Lookup(ctx, sessionSubject, PredSessionEnded)

	includes, _ := k.graph.Store().BySubjectPredicate(ctx, sessionSubject, PredSessionInclude)
	s.Includes = includes

	excludes, _ := k.graph.Store().BySubjectPredicate(ctx, sessionSubject, PredSessionExclude)
	s.Excludes = excludes

	return s, nil
}

// SaveCurrentSession writes the active session to the DB using a
// well-known subject. Only one session is active at a time.
func (k *KB) SaveCurrentSession(ctx context.Context, root, nodeTitle string, includes, excludes []string) error {
	// Clear previous session
	k.graph.Store().RetractBySubject(ctx, CurrentSessionSubject)

	triples := []triple.Triple{
		{Subject: CurrentSessionSubject, Predicate: PredSessionRoot, Object: root},
		{Subject: CurrentSessionSubject, Predicate: PredSessionFocus, Object: nodeTitle},
		{Subject: CurrentSessionSubject, Predicate: PredSessionStarted, Object: time.Now().UTC().Format(time.RFC3339)},
	}
	for _, inc := range includes {
		triples = append(triples, triple.Triple{Subject: CurrentSessionSubject, Predicate: PredSessionInclude, Object: inc})
	}
	for _, exc := range excludes {
		triples = append(triples, triple.Triple{Subject: CurrentSessionSubject, Predicate: PredSessionExclude, Object: exc})
	}
	return k.graph.Store().AssertBatch(ctx, triples)
}

// LoadCurrentSession reads the active session from the DB.
// Returns nil if no session is active.
func (k *KB) LoadCurrentSession(ctx context.Context) (*Session, error) {
	focus, ok, err := k.graph.Lookup(ctx, CurrentSessionSubject, PredSessionFocus)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	root, _, _ := k.graph.Lookup(ctx, CurrentSessionSubject, PredSessionRoot)
	started, _, _ := k.graph.Lookup(ctx, CurrentSessionSubject, PredSessionStarted)
	includes, _ := k.graph.Store().BySubjectPredicate(ctx, CurrentSessionSubject, PredSessionInclude)
	excludes, _ := k.graph.Store().BySubjectPredicate(ctx, CurrentSessionSubject, PredSessionExclude)

	return &Session{
		Subject:  CurrentSessionSubject,
		Focus:    focus,
		Includes: includes,
		Excludes: excludes,
		Started:  started,
		Root:     root,
	}, nil
}

// ClearCurrentSession removes the active session.
func (k *KB) ClearCurrentSession(ctx context.Context) error {
	return k.graph.Store().RetractBySubject(ctx, CurrentSessionSubject)
}

func sessionSubject() string {
	ts := time.Now().UTC().Format("20060102T150405")
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("session:%s:%x", ts, b)
}
