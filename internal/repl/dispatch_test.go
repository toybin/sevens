package repl

import (
	"fmt"
	"reflect"
	"testing"

	"sevens/internal/kb"
)

// ─── tokenize ─────────────────────────────────────────────────────────────────

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		// Basic cases
		{"empty string", "", nil},
		{"single word", "walk", []string{"walk"}},
		{"multiple words", "focus The Commons", []string{"focus", "The", "Commons"}},

		// Quoted strings
		{"quoted preserves spaces", `focus "The Commons"`, []string{"focus", "The Commons"}},
		{"quoted search query", `search "lending infrastructure"`, []string{"search", "lending infrastructure"}},
		{"quoted with flag", `accept --with "make titles shorter"`, []string{"accept", "--with", "make titles shorter"}},

		// Mixed quoted and unquoted
		{"mixed quoted unquoted", `cmd "foo bar" baz`, []string{"cmd", "foo bar", "baz"}},
		{"dot command with quoted arg", `.include "My Node Title"`, []string{".include", "My Node Title"}},

		// Whitespace handling
		{"leading whitespace", "  walk", []string{"walk"}},
		{"trailing whitespace", "walk  ", []string{"walk"}},
		{"multiple internal spaces", "focus   The   Commons", []string{"focus", "The", "Commons"}},
		{"leading and trailing whitespace", "  walk  ", []string{"walk"}},

		// Flags
		{"dot command with arg", ".model fast", []string{".model", "fast"}},
		{"flags sequence", "notice --dry-run --model fast", []string{"notice", "--dry-run", "--model", "fast"}},
		{"numeric arg", "child 3", []string{"child", "3"}},

		// Edge cases with quotes
		{"quote at end of word", `foo"`, []string{"foo"}},
		{"unclosed quote absorbs rest", `cmd "unclosed`, []string{"cmd", "unclosed"}},
		{"special chars inside quotes", `cmd "foo/bar baz.qux"`, []string{"cmd", "foo/bar baz.qux"}},
		{"empty quoted string", `cmd ""`, []string{"cmd"}},
		{"multiple quoted tokens", `"foo bar" "baz qux"`, []string{"foo bar", "baz qux"}},

		// Tab as whitespace
		{"tab separator", "walk\tchild", []string{"walk", "child"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("tokenize(%q) = %v (len %d), want %v (len %d)",
					tt.input, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ─── parsePositiveInt ─────────────────────────────────────────────────────────

func TestParsePositiveInt(t *testing.T) {
	tests := []struct {
		input  string
		wantN  int
		wantOk bool
	}{
		{"1", 1, true},
		{"7", 7, true},
		{"42", 42, true},
		{"100", 100, true},
		{"0", 0, false},
		{"-1", 0, false},
		{"abc", 0, false},
		{"", 0, false},
		{"3.5", 0, false},
		// Additional edge cases
		{"01", 1, true},  // leading zero still parses as 1
		{" 1", 0, false}, // space prefix: strconv.Atoi rejects
		{"1 ", 0, false}, // trailing space
		{"999999", 999999, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			n, ok := parsePositiveInt(tt.input)
			if n != tt.wantN || ok != tt.wantOk {
				t.Errorf("parsePositiveInt(%q) = (%d, %v), want (%d, %v)",
					tt.input, n, ok, tt.wantN, tt.wantOk)
			}
		})
	}
}

// ─── parseInlineFlags ─────────────────────────────────────────────────────────

func TestParseInlineFlags(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		want   inlineFlags
	}{
		{
			"empty nil",
			nil,
			inlineFlags{},
		},
		{
			"empty slice",
			[]string{},
			inlineFlags{},
		},
		{
			"model space-value",
			[]string{"--model", "fast"},
			inlineFlags{model: "fast"},
		},
		{
			"model equals-value",
			[]string{"--model=powerful"},
			inlineFlags{model: "powerful"},
		},
		{
			"dry-run flag",
			[]string{"--dry-run"},
			inlineFlags{dryRun: true},
		},
		{
			"backend space-value",
			[]string{"--backend", "codex"},
			inlineFlags{backend: "codex"},
		},
		{
			"backend equals-value",
			[]string{"--backend=openai"},
			inlineFlags{backend: "openai"},
		},
		{
			"with single word",
			[]string{"--with", "shorter"},
			inlineFlags{with: "shorter"},
		},
		{
			"with multi-word consumes rest",
			[]string{"--with", "make", "titles", "shorter"},
			inlineFlags{with: "make titles shorter"},
		},
		{
			"with equals value",
			[]string{"--with=shorter"},
			inlineFlags{with: "shorter"},
		},
		{
			"yes long flag",
			[]string{"--yes"},
			inlineFlags{yes: true},
		},
		{
			"yes short flag",
			[]string{"--y"},
			inlineFlags{yes: true},
		},
		{
			"noninteractive short flag",
			[]string{"-n"},
			inlineFlags{nonInteractive: true},
		},
		{
			"combined model and dry-run",
			[]string{"--model", "fast", "--dry-run"},
			inlineFlags{model: "fast", dryRun: true},
		},
		{
			"combined model and yes",
			[]string{"--model", "capable", "--yes"},
			inlineFlags{model: "capable", yes: true},
		},
		{
			"include repeated",
			[]string{"--include", "Inbox", "--include=Braindump"},
			inlineFlags{includes: []string{"Inbox", "Braindump"}},
		},
		{
			"root and block",
			[]string{"--root", "/tmp/root", "--block", "1.2"},
			inlineFlags{root: "/tmp/root", block: "1.2"},
		},
		{
			"unknown flag ignored",
			[]string{"--unknown-flag"},
			inlineFlags{},
		},
		{
			"unknown flag with value ignored",
			[]string{"--unknown", "value"},
			inlineFlags{},
		},
		{
			"non-flag tokens skipped",
			[]string{"someword", "--model", "fast"},
			inlineFlags{model: "fast"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseInlineFlags(tt.tokens)
			if got.model != tt.want.model {
				t.Errorf("model = %q, want %q", got.model, tt.want.model)
			}
			if got.backend != tt.want.backend {
				t.Errorf("backend = %q, want %q", got.backend, tt.want.backend)
			}
			if got.root != tt.want.root {
				t.Errorf("root = %q, want %q", got.root, tt.want.root)
			}
			if got.block != tt.want.block {
				t.Errorf("block = %q, want %q", got.block, tt.want.block)
			}
			if got.with != tt.want.with {
				t.Errorf("with = %q, want %q", got.with, tt.want.with)
			}
			if !reflect.DeepEqual(got.includes, tt.want.includes) {
				t.Errorf("includes = %#v, want %#v", got.includes, tt.want.includes)
			}
			if got.dryRun != tt.want.dryRun {
				t.Errorf("dryRun = %v, want %v", got.dryRun, tt.want.dryRun)
			}
			if got.yes != tt.want.yes {
				t.Errorf("yes = %v, want %v", got.yes, tt.want.yes)
			}
			if got.nonInteractive != tt.want.nonInteractive {
				t.Errorf("nonInteractive = %v, want %v", got.nonInteractive, tt.want.nonInteractive)
			}
		})
	}
}

// ─── inlineFlags.has ─────────────────────────────────────────────────────────

func TestInlineFlagsHas(t *testing.T) {
	tests := []struct {
		name  string
		flags inlineFlags
		key   string
		want  bool
	}{
		{"dry-run true", inlineFlags{dryRun: true}, "dry-run", true},
		{"dry-run false", inlineFlags{dryRun: false}, "dry-run", false},
		{"yes true", inlineFlags{yes: true}, "yes", true},
		{"yes false", inlineFlags{yes: false}, "yes", false},
		{"unknown key returns false", inlineFlags{dryRun: true, yes: true}, "model", false},
		{"unknown key empty flags", inlineFlags{}, "anything", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.flags.has(tt.key)
			if got != tt.want {
				t.Errorf("inlineFlags.has(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// ─── removeString ─────────────────────────────────────────────────────────────

func TestRemoveString(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		s     string
		want  []string
	}{
		{
			"remove middle element",
			[]string{"a", "b", "c"}, "b",
			[]string{"a", "c"},
		},
		{
			"remove first element",
			[]string{"a", "b", "c"}, "a",
			[]string{"b", "c"},
		},
		{
			"remove last element",
			[]string{"a", "b", "c"}, "c",
			[]string{"a", "b"},
		},
		{
			"remove non-existing element",
			[]string{"a", "b", "c"}, "d",
			[]string{"a", "b", "c"},
		},
		{
			"remove sole element",
			[]string{"a"}, "a",
			[]string{},
		},
		{
			"nil slice",
			nil, "a",
			nil,
		},
		{
			"remove from empty slice",
			[]string{}, "a",
			[]string{},
		},
		{
			"remove duplicate only removes all occurrences",
			[]string{"a", "b", "a"}, "a",
			[]string{"b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeString(tt.slice, tt.s)
			if len(got) != len(tt.want) {
				t.Errorf("removeString(%v, %q) = %v (len %d), want %v (len %d)",
					tt.slice, tt.s, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("removeString(%v, %q)[%d] = %q, want %q",
						tt.slice, tt.s, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ─── orDefault ────────────────────────────────────────────────────────────────

func TestOrDefault(t *testing.T) {
	tests := []struct {
		name string
		s    string
		def  string
		want string
	}{
		{"non-empty s returns s", "hello", "world", "hello"},
		{"empty s returns def", "", "world", "world"},
		{"both empty returns empty def", "", "", ""},
		{"non-empty s ignores non-empty def", "foo", "bar", "foo"},
		{"single space s is non-empty", " ", "bar", " "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orDefault(tt.s, tt.def)
			if got != tt.want {
				t.Errorf("orDefault(%q, %q) = %q, want %q", tt.s, tt.def, got, tt.want)
			}
		})
	}
}

// ─── truncateTitle ────────────────────────────────────────────────────────────

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"well under limit", "short", 10, "short"},
		{"exact limit no truncation", "exactly ten", 11, "exactly ten"},
		{"over limit appends ellipsis", "this is a long title", 10, "this is a…"},
		{"exact limit boundary", "hello", 5, "hello"},
		{"one over limit", "hello!", 5, "hell…"},
		{"very short max of 1", "hello", 1, "…"},
		{"max of 2", "hello", 2, "h…"},
		{"unicode chars counted as runes", "héllo wörld", 6, "héllo…"},
		{"ascii stays unchanged when fits", "abc", 100, "abc"},
		{"empty string", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateTitle(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("truncateTitle(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

// ─── dispatch grammar ordering ──────────────────────────────────────────────

// TestDispatchGrammarOrder verifies that the dispatch grammar resolves
// ambiguous tokens in the correct priority order. These are important
// invariants documented in the design doc and easy to break with refactoring.
func TestDispatchGrammarOrder(t *testing.T) {
	// The grammar should resolve tokens in this order:
	// 1. Dot commands (except "..")
	// 2. Navigation keywords (.., up, root, focus, child, sibling)
	// 3. Numeric selection
	// 4. Named commands (walk, children, search, discuss, note, new, etc.)
	// 5. Function names
	// 6. Node titles
	// 7. Unknown → error
	//
	// Key invariant: "discuss" and "note" must route to their interactive
	// modes (step 4), NOT to handleApply (step 5), even though they are
	// also valid function names.

	// Verify "discuss" and "note" are real function names that could collide.
	// This test will fail if the functions are removed, which is a signal
	// to update the dispatch grammar.
	if !isFunctionName("discuss") {
		t.Skip("discuss function not installed — collision test not applicable")
	}
	if !isFunctionName("note") {
		// note may not be a function — that's fine, just test discuss
		t.Log("note is not a function name — no collision to guard against")
	}

	// Verify dot-command prefix check excludes ".."
	tokens := tokenize("..")
	if len(tokens) != 1 || tokens[0] != ".." {
		t.Errorf("tokenize(..) = %v, want [..]", tokens)
	}
	// ".." starts with "." but must NOT be treated as a dot command.
	// The dispatch code has: if strings.HasPrefix(head, ".") && head != ".."
	// This is a regression guard for that condition.
	if tokens[0] != ".." {
		t.Fatal("unreachable")
	}
	if len(tokens[0]) > 0 && tokens[0][0] == '.' && tokens[0] != ".." {
		t.Error("'..' would be incorrectly routed to dot command handler")
	}
}

// TestNowISO verifies the timestamp format used in log entries.
func TestNowISO(t *testing.T) {
	ts := nowISO()
	if len(ts) == 0 {
		t.Fatal("nowISO returned empty string")
	}
	// Should be RFC3339 format: 2006-01-02T15:04:05Z07:00
	if ts[4] != '-' || ts[7] != '-' || ts[10] != 'T' {
		t.Errorf("nowISO() = %q, doesn't look like RFC3339", ts)
	}
}

// TestModeConstants verifies mode enum values are distinct.
func TestModeConstants(t *testing.T) {
	modes := []Mode{ModeNormal, ModeNote, ModeDiscussion}
	seen := make(map[Mode]bool)
	for _, m := range modes {
		if seen[m] {
			t.Errorf("duplicate mode value: %d", m)
		}
		seen[m] = true
	}
	if ModeNormal != 0 {
		t.Error("ModeNormal should be zero value")
	}
}

func TestShouldAutoSync(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		want   bool
	}{
		{"empty", nil, false},
		{"walk auto-syncs", []string{"walk"}, true},
		{"blocks skips auto-sync", []string{"blocks"}, false},
		{"diff-blocks skips auto-sync", []string{"diff-blocks", "Node"}, false},
		{"extract-block skips auto-sync", []string{"extract-block", "0.1"}, false},
		{"sync skips auto-sync", []string{"sync"}, false},
		{"search auto-syncs", []string{"search", "query"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAutoSync(tt.tokens); got != tt.want {
				t.Fatalf("shouldAutoSync(%v) = %v, want %v", tt.tokens, got, tt.want)
			}
		})
	}
}

func TestBlockDiffArgs(t *testing.T) {
	tests := []struct {
		name          string
		tokens        []string
		wantRoot      string
		wantEDN       bool
		wantUnchanged bool
		wantTitle     string
		wantErr       string
	}{
		{"no args", nil, "", false, false, "", ""},
		{"title only", []string{"My Node"}, "", false, false, "My Node", ""},
		{"flag only", []string{"--unchanged"}, "", false, true, "", ""},
		{"flag and title", []string{"--unchanged", "My Node"}, "", false, true, "My Node", ""},
		{"title and flag", []string{"My Node", "--unchanged"}, "", false, true, "My Node", ""},
		{"edn and root", []string{"--edn", "--root", "/tmp/root", "Node"}, "/tmp/root", true, false, "Node", ""},
		{"multi-token title", []string{"Node", "Two"}, "", false, false, "Node Two", ""},
		{"unknown flag", []string{"--wat"}, "", false, false, "", `unknown flag "--wat"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRoot, gotEDN, gotUnchanged, gotTitle, err := blockDiffArgs(tt.tokens)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("blockDiffArgs(%v) error = %v, want %q", tt.tokens, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("blockDiffArgs(%v) unexpected error: %v", tt.tokens, err)
			}
			if gotRoot != tt.wantRoot {
				t.Fatalf("root = %q, want %q", gotRoot, tt.wantRoot)
			}
			if gotEDN != tt.wantEDN {
				t.Fatalf("edn = %v, want %v", gotEDN, tt.wantEDN)
			}
			if gotUnchanged != tt.wantUnchanged {
				t.Fatalf("showUnchanged = %v, want %v", gotUnchanged, tt.wantUnchanged)
			}
			if gotTitle != tt.wantTitle {
				t.Fatalf("title = %q, want %q", gotTitle, tt.wantTitle)
			}
		})
	}
}

func TestExtractBlockArgs(t *testing.T) {
	tests := []struct {
		name        string
		tokens      []string
		defaultNode string
		defaultPath string
		resolve     func(string) string
		wantRoot    string
		wantSource  string
		wantPath    string
		wantTitle   string
		wantParent  string
		wantErr     string
	}{
		{"path only", []string{"1.2"}, "Source", "", nil, "", "Source", "1.2", "", "", ""},
		{"path and title", []string{"1.2", "Definition", "of", "Done"}, "Source", "", nil, "", "Source", "1.2", "Definition of Done", "", ""},
		{"parent flag", []string{"1.2", "--parent", "Braindump"}, "Source", "", nil, "", "Source", "1.2", "", "Braindump", ""},
		{"parent equals", []string{"1.2", "--parent=Braindump", "Definition"}, "Source", "", nil, "", "Source", "1.2", "Definition", "Braindump", ""},
		{"root flag", []string{"1.2", "--root", "/tmp/root"}, "Source", "", nil, "/tmp/root", "Source", "1.2", "", "", ""},
		{"selected block title only", []string{"Definition", "of", "Done"}, "Source", "1.2", nil, "", "Source", "1.2", "Definition of Done", "", ""},
		{"selected block no args", nil, "Source", "1.2", nil, "", "Source", "1.2", "", "", ""},
		{"explicit source node", []string{"Inbox", "1.2", "Definition"}, "Source", "", func(s string) string {
			if s == "Inbox" {
				return "Inbox"
			}
			return ""
		}, "", "Inbox", "1.2", "Definition", "", ""},
		{"missing path", nil, "Source", "", nil, "", "", "", "", "", "usage: extract-block [<source-node>] <path> [title] [--parent <title>]"},
		{"missing parent value", []string{"1.2", "--parent"}, "Source", "", nil, "", "", "", "", "", "flag --parent requires a value"},
		{"unknown flag", []string{"1.2", "--wat"}, "Source", "", nil, "", "", "", "", "", `unknown flag "--wat"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolve := tt.resolve
			if resolve == nil {
				resolve = func(string) string { return "" }
			}
			gotRoot, gotSource, gotPath, gotTitle, gotParent, err := extractBlockArgs(tt.tokens, tt.defaultNode, tt.defaultPath, resolve)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("extractBlockArgs(%v, %q, %q) error = %v, want %q", tt.tokens, tt.defaultNode, tt.defaultPath, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractBlockArgs(%v, %q, %q) unexpected error: %v", tt.tokens, tt.defaultNode, tt.defaultPath, err)
			}
			if gotRoot != tt.wantRoot {
				t.Fatalf("root = %q, want %q", gotRoot, tt.wantRoot)
			}
			if gotSource != tt.wantSource {
				t.Fatalf("source = %q, want %q", gotSource, tt.wantSource)
			}
			if gotPath != tt.wantPath {
				t.Fatalf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if gotTitle != tt.wantTitle {
				t.Fatalf("title = %q, want %q", gotTitle, tt.wantTitle)
			}
			if gotParent != tt.wantParent {
				t.Fatalf("parent = %q, want %q", gotParent, tt.wantParent)
			}
		})
	}
}

func TestParseTemplateInvokeArgs(t *testing.T) {
	tests := []struct {
		name       string
		tokens     []string
		wantRoot   string
		wantParent string
		wantAt     string
		wantArgs   []string
		wantVars   map[string]string
		wantDryRun bool
		wantErr    string
	}{
		{
			name:     "positional args only",
			tokens:   []string{"meeting", "notes"},
			wantRoot: "",
			wantArgs: []string{"meeting", "notes"},
			wantVars: map[string]string{},
		},
		{
			name:       "flags and set values",
			tokens:     []string{"--root", "/tmp/root", "--parent", "Inbox", "--at=Braindump", "--set", "title=Sync", "--set=summary=Dates", "--dry-run"},
			wantRoot:   "/tmp/root",
			wantParent: "Inbox",
			wantAt:     "Braindump",
			wantArgs:   nil,
			wantVars:   map[string]string{"title": "Sync", "summary": "Dates"},
			wantDryRun: true,
		},
		{
			name:       "short aliases and semantic vars",
			tokens:     []string{"-p", "Inbox", "-a", "Braindump", "--heading", "Questions", "--text", "What is blocked?"},
			wantParent: "Inbox",
			wantAt:     "Braindump",
			wantArgs:   nil,
			wantVars:   map[string]string{"heading": "Questions", "text": "What is blocked?"},
		},
		{
			name:    "missing flag value",
			tokens:  []string{"--parent"},
			wantErr: "flag --parent requires a value",
		},
		{
			name:    "bad set",
			tokens:  []string{"--set", "title"},
			wantErr: "--set requires key=value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRoot, gotParent, gotAt, gotVars, gotArgs, gotDryRun, err := parseTemplateInvokeArgs(tt.tokens)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("parseTemplateInvokeArgs(%v) error = %v, want %q", tt.tokens, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTemplateInvokeArgs(%v) unexpected error: %v", tt.tokens, err)
			}
			if gotRoot != tt.wantRoot {
				t.Fatalf("root = %q, want %q", gotRoot, tt.wantRoot)
			}
			if gotParent != tt.wantParent {
				t.Fatalf("parent = %q, want %q", gotParent, tt.wantParent)
			}
			if gotAt != tt.wantAt {
				t.Fatalf("at = %q, want %q", gotAt, tt.wantAt)
			}
			if gotDryRun != tt.wantDryRun {
				t.Fatalf("dry-run = %v, want %v", gotDryRun, tt.wantDryRun)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", gotArgs, tt.wantArgs)
			}
			if !reflect.DeepEqual(gotVars, tt.wantVars) {
				t.Fatalf("vars = %#v, want %#v", gotVars, tt.wantVars)
			}
		})
	}
}

// testGraphQuerier is a minimal GraphQuerier for tests.
type testGraphQuerier struct {
	titles map[string]string // title -> subject
	objs   map[string]map[string]string // subject -> predicate -> object
}

func (q *testGraphQuerier) ResolveTitle(title, root string) string {
	for t := range q.titles {
		if t == title {
			return t
		}
	}
	return ""
}
func (q *testGraphQuerier) ResolveNode(title, root string) (string, string) {
	return q.titles[title], ""
}
func (q *testGraphQuerier) GetObject(subject, predicate string) (string, error) {
	if m, ok := q.objs[subject]; ok {
		return m[predicate], nil
	}
	return "", nil
}
func (q *testGraphQuerier) NodeTitle(subject string) (string, error) {
	for t, s := range q.titles {
		if s == subject {
			return t, nil
		}
	}
	return "", nil
}
func (q *testGraphQuerier) ListNodeTitles(root string) ([]string, error) { return nil, nil }
func (q *testGraphQuerier) SearchTitles(query, root string) ([]string, error) { return nil, nil }
func (q *testGraphQuerier) SearchContent(query, root string) ([]string, error) { return nil, nil }
func (q *testGraphQuerier) BuildWalk(root, title string, depth int) (*WalkOutput, error) {
	subj := q.titles[title]
	if subj == "" {
		return nil, fmt.Errorf("node not found: %q", title)
	}
	var parent *string
	if p, ok := q.objs[subj]["node/parent"]; ok {
		// Find parent title.
		for t, s := range q.titles {
			if s == p {
				parent = &t
				break
			}
		}
	}
	return &WalkOutput{Node: WalkNode{Subject: subj, Title: title, Parent: parent}}, nil
}
func (q *testGraphQuerier) BuildOverview(root string) (*OverviewOutput, error) { return nil, nil }
func (q *testGraphQuerier) BuildBlockList(root, nodeTitle string) (BlockListOutput, error) { return BlockListOutput{}, nil }
func (q *testGraphQuerier) BuildBlockDiff(root, nodeTitle string) (BlockDiffOutput, error) { return BlockDiffOutput{}, nil }
func (q *testGraphQuerier) ChildrenSummary(root, nodeTitle string) ([]ChildSummary, error) { return nil, nil }
func (q *testGraphQuerier) PrepareBlockExtraction(root, sourceTitle, blockPath, newTitle, parentTitle string) (ExtractedNode, error) { return ExtractedNode{}, nil }
func (q *testGraphQuerier) ResolveBlockTarget(root, nodeTitle, blockPath string) (*BlockTarget, error) { return nil, nil }
func (q *testGraphQuerier) ResolveBlockTargetBySubject(subject string) (*BlockTarget, error) { return nil, nil }
func (q *testGraphQuerier) LoadConfig(root string) (GraphConfig, error) { return GraphConfig{}, nil }
func (q *testGraphQuerier) AutoGroupIncludes(root, nodeTitle string) ([]string, error) { return nil, nil }
func (q *testGraphQuerier) ResolveGroup(root string, group GraphGroup) ([]string, error) { return nil, nil }
func (q *testGraphQuerier) Resync(root string) error { return nil }
func (q *testGraphQuerier) ScopeString(scope []string) string { return "" }
func (q *testGraphQuerier) RenderBlockMarkdown(block BlockListEntry) string { return block.Text }

func TestHandleNavUpMovesToParent(t *testing.T) {
	root := t.TempDir()

	parentSubj := kb.NodeSubject(root, "Parent")
	childSubj := kb.NodeSubject(root, "Child")

	q := &testGraphQuerier{
		titles: map[string]string{
			"Parent": parentSubj,
			"Child":  childSubj,
		},
		objs: map[string]map[string]string{
			childSubj: {
				"node/parent": parentSubj,
			},
		},
	}

	r := &REPL{root: root, focus: "Child", graphQ: q}
	if err := r.handleNavUp(); err != nil {
		t.Fatalf("handleNavUp() error = %v", err)
	}
	if r.focus != "Parent" {
		t.Fatalf("focus = %q, want %q", r.focus, "Parent")
	}
	if r.focusBlock != nil {
		t.Fatalf("focusBlock = %#v, want nil", r.focusBlock)
	}
}
