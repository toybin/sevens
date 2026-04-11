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
	Focus    string   // node subject
	Includes []string // node subjects
	Excludes []string // node subjects
	Started  string
	Ended    string
}

// StartSession creates a new session with a focus node.
func (k *KB) StartSession(ctx context.Context, focusSubject string) (*Session, error) {
	subject := sessionSubject()
	ts := time.Now().UTC().Format(time.RFC3339)

	triples := []triple.Triple{
		{subject, PredSessionFocus, focusSubject},
		{subject, PredSessionStarted, ts},
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

func sessionSubject() string {
	ts := time.Now().UTC().Format("20060102T150405")
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("session:%s:%x", ts, b)
}
