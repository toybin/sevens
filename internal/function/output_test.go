package function

import (
	"testing"
)

// TestParseOutput_EmptyOpsEnvelope verifies that a JSON object of
// the form {"ops":[]} parses as a legitimate empty-result envelope
// for a FileOps-shaped step, not as arbitrary text. This is the
// "distill found nothing to do" case — the LLM correctly produced
// an empty JSON envelope to mean "no edits needed."
func TestParseOutput_EmptyOpsEnvelope(t *testing.T) {
	env, err := ParseOutput(`{"ops":[]}`, ShapeFileOps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil envelope")
	}
	if len(env.Ops) != 0 {
		t.Errorf("expected empty ops, got %v", env.Ops)
	}
	if env.Text != "" {
		t.Errorf("empty-ops envelope should not produce text fallback, got %q", env.Text)
	}
}

// TestParseOutput_EmptyOpsEnvelopeWithWhitespace verifies the same
// recognition when the envelope has surrounding whitespace or
// newlines (LLMs sometimes pretty-print).
func TestParseOutput_EmptyOpsEnvelopeWithWhitespace(t *testing.T) {
	env, err := ParseOutput("  {\n  \"ops\": []\n}  ", ShapeFileOps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Text != "" {
		t.Errorf("expected empty-ops, not text: %q", env.Text)
	}
}

// TestParseOutput_EmptyOpsWithOtherShapes verifies that an empty
// envelope for the wrong shape does NOT get silently coerced —
// {"ops":[]} against a ShapeText step should fall through to the
// text fallback since the shape mismatches the envelope key.
func TestParseOutput_EmptyOpsDoesNotMatchText(t *testing.T) {
	env, err := ParseOutput(`{"ops":[]}`, ShapeText)
	_ = err
	if env == nil {
		t.Fatal("expected non-nil envelope")
	}
	// For ShapeText, an empty envelope with "ops" key should
	// canonicalize to an empty Text — matches "no output".
	if env.Text != "" {
		t.Errorf("expected empty text for empty envelope against ShapeText, got %q", env.Text)
	}
}

// TestParseOutput_NonEnvelopeTextStillFalls verifies that raw text
// that happens to parse as empty JSON (e.g. `{}`) but doesn't
// contain any envelope keys still falls through to the text
// fallback rather than being treated as an empty envelope.
func TestParseOutput_NonEnvelopeJSONFallsThrough(t *testing.T) {
	env, err := ParseOutput(`{}`, ShapeText)
	_ = err
	if env == nil {
		t.Fatal("expected non-nil envelope")
	}
	if env.Text != "{}" {
		t.Errorf("expected raw text `{}`, got %q", env.Text)
	}
}
