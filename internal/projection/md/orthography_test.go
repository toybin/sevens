package md

import (
	"testing"
)

// --- TokenRegistry ---

func TestDefaultRegistryContents(t *testing.T) {
	reg := DefaultRegistry()
	expected := map[string]bool{"#": false, "@": false, "!": false, "~": false, "::": false}
	for _, sig := range reg.Signifiers {
		if _, ok := expected[sig]; ok {
			expected[sig] = true
		}
	}
	for sig, found := range expected {
		if !found {
			t.Errorf("default registry missing signifier %q", sig)
		}
	}
}

func TestRegistrySortedLongestFirst(t *testing.T) {
	reg := NewTokenRegistry([]string{":", "::", ":::"})
	if reg.Signifiers[0] != ":::" || reg.Signifiers[1] != "::" || reg.Signifiers[2] != ":" {
		t.Fatalf("expected sorted longest-first, got %v", reg.Signifiers)
	}
}

// --- Slot classification (arity) ---

func TestSlotArity0BareWord(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("x", reg)
	assertSlot(t, s, ArityZero, "x", "", "")
}

func TestSlotArity0BareSignifier(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("!", reg)
	assertSlot(t, s, ArityZero, "!", "", "")
}

func TestSlotArity0MultiCharSignifier(t *testing.T) {
	reg := DefaultRegistry()
	// "::blocked" with "::" registered — this is arity-1: signifier fused with symbol.
	// A bare "::" with nothing after would be arity-0.
	s := classifySlot("::", reg)
	assertSlot(t, s, ArityZero, "::", "", "")
}

func TestSlotArity1FusedAt(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("@julian", reg)
	assertSlot(t, s, ArityOne, "@", "julian", "")
}

func TestSlotArity1FusedHash(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("#research", reg)
	assertSlot(t, s, ArityOne, "#", "research", "")
}

func TestSlotArity1FusedTilde(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("~2h", reg)
	assertSlot(t, s, ArityOne, "~", "2h", "")
}

func TestSlotArity1MultiCharSignifier(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("::blocked", reg)
	assertSlot(t, s, ArityOne, "::", "blocked", "")
}

func TestSlotArity2WordKeyPayload(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("status done", reg)
	assertSlot(t, s, ArityTwo, "status", "", "done")
}

func TestSlotArity2SignifierSpacePayload(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("@ julian", reg)
	assertSlot(t, s, ArityTwo, "@", "", "julian")
}

func TestSlotArity2AssigneeWithRef(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("assignee @julian", reg)
	assertSlot(t, s, ArityTwo, "assignee", "", "@julian")
}

func TestSlotArity2DueDate(t *testing.T) {
	reg := DefaultRegistry()
	s := classifySlot("due 2026-04-08", reg)
	assertSlot(t, s, ArityTwo, "due", "", "2026-04-08")
}

// --- ParsePropertyList ---

func TestParsePropertyListSingle(t *testing.T) {
	reg := DefaultRegistry()
	slots := ParsePropertyList("x", reg)
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}
	assertSlot(t, slots[0], ArityZero, "x", "", "")
}

func TestParsePropertyListMultiple(t *testing.T) {
	reg := DefaultRegistry()
	slots := ParsePropertyList("status done | @julian | #research", reg)
	if len(slots) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(slots))
	}
	assertSlot(t, slots[0], ArityTwo, "status", "", "done")
	assertSlot(t, slots[1], ArityOne, "@", "julian", "")
	assertSlot(t, slots[2], ArityOne, "#", "research", "")
}

func TestParsePropertyListMultiline(t *testing.T) {
	reg := DefaultRegistry()
	slots := ParsePropertyList("\n| status done\n| @julian\n", reg)
	if len(slots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(slots))
	}
	assertSlot(t, slots[0], ArityTwo, "status", "", "done")
	assertSlot(t, slots[1], ArityOne, "@", "julian", "")
}

func TestParsePropertyListMixed(t *testing.T) {
	reg := DefaultRegistry()
	slots := ParsePropertyList("x\n| @julian\n| #research", reg)
	if len(slots) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(slots))
	}
	assertSlot(t, slots[0], ArityZero, "x", "", "")
	assertSlot(t, slots[1], ArityOne, "@", "julian", "")
	assertSlot(t, slots[2], ArityOne, "#", "research", "")
}

func TestParsePropertyListEmpty(t *testing.T) {
	reg := DefaultRegistry()
	slots := ParsePropertyList("", reg)
	if len(slots) != 0 {
		t.Fatalf("expected 0 slots, got %d", len(slots))
	}
}

// --- FindPropertyLists ---

func TestFindPropertyListsInlineHeadingPrefix(t *testing.T) {
	reg := DefaultRegistry()
	pls := FindPropertyLists("# (!) Heading", nil, reg)
	if len(pls) != 1 {
		t.Fatalf("expected 1 property list, got %d", len(pls))
	}
	if len(pls[0].Slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(pls[0].Slots))
	}
	assertSlot(t, pls[0].Slots[0], ArityZero, "!", "", "")
}

func TestFindPropertyListsInlineHeadingSuffix(t *testing.T) {
	reg := DefaultRegistry()
	pls := FindPropertyLists("# Heading (status done)", nil, reg)
	if len(pls) != 1 {
		t.Fatalf("expected 1 property list, got %d", len(pls))
	}
	assertSlot(t, pls[0].Slots[0], ArityTwo, "status", "", "done")
}

func TestFindPropertyListsFollowingLineHeading(t *testing.T) {
	reg := DefaultRegistry()
	pls := FindPropertyLists("# Heading", []string{"(status done)"}, reg)
	if len(pls) != 1 {
		t.Fatalf("expected 1 property list, got %d", len(pls))
	}
	assertSlot(t, pls[0].Slots[0], ArityTwo, "status", "", "done")
}

func TestFindPropertyListsInlineListItemPrefix(t *testing.T) {
	reg := DefaultRegistry()
	pls := FindPropertyLists("- (x) item", nil, reg)
	if len(pls) != 1 {
		t.Fatalf("expected 1 property list, got %d", len(pls))
	}
	assertSlot(t, pls[0].Slots[0], ArityZero, "x", "", "")
}

func TestFindPropertyListsInlineListItemSuffix(t *testing.T) {
	reg := DefaultRegistry()
	pls := FindPropertyLists("- item (status done)", nil, reg)
	if len(pls) != 1 {
		t.Fatalf("expected 1 property list, got %d", len(pls))
	}
	assertSlot(t, pls[0].Slots[0], ArityTwo, "status", "", "done")
}

func TestFindPropertyListsContinuationListItem(t *testing.T) {
	reg := DefaultRegistry()
	pls := FindPropertyLists("- item text", []string{"  (status done)"}, reg)
	if len(pls) != 1 {
		t.Fatalf("expected 1 property list, got %d", len(pls))
	}
	assertSlot(t, pls[0].Slots[0], ArityTwo, "status", "", "done")
}

func TestFindPropertyListsNotInProse(t *testing.T) {
	reg := DefaultRegistry()
	pls := FindPropertyLists("This is (not a property list)", nil, reg)
	if len(pls) != 0 {
		t.Fatalf("expected 0 property lists in prose, got %d", len(pls))
	}
}

func TestFindPropertyListsMultilineContinuation(t *testing.T) {
	reg := DefaultRegistry()
	pls := FindPropertyLists("# Heading", []string{
		"(",
		"| status done",
		"| @julian",
		")",
	}, reg)
	if len(pls) != 1 {
		t.Fatalf("expected 1 property list, got %d", len(pls))
	}
	if len(pls[0].Slots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(pls[0].Slots))
	}
	assertSlot(t, pls[0].Slots[0], ArityTwo, "status", "", "done")
	assertSlot(t, pls[0].Slots[1], ArityOne, "@", "julian", "")
}

// --- FindInlineAtoms ---

func TestFindInlineAtomsBasic(t *testing.T) {
	reg := DefaultRegistry()
	atoms := FindInlineAtoms("mentions @julian and #research", reg)
	if len(atoms) != 2 {
		t.Fatalf("expected 2 atoms, got %d", len(atoms))
	}
	assertAtom(t, atoms[0], "@", "julian")
	assertAtom(t, atoms[1], "#", "research")
}

func TestFindInlineAtomsCodeShielding(t *testing.T) {
	reg := DefaultRegistry()
	atoms := FindInlineAtoms("use `@julian` in code", reg)
	if len(atoms) != 0 {
		t.Fatalf("expected 0 atoms inside code span, got %d", len(atoms))
	}
}

func TestFindInlineAtomsMultiCharSignifier(t *testing.T) {
	reg := DefaultRegistry()
	atoms := FindInlineAtoms("state is ::blocked", reg)
	if len(atoms) != 1 {
		t.Fatalf("expected 1 atom, got %d", len(atoms))
	}
	assertAtom(t, atoms[0], "::", "blocked")
}

func TestFindInlineAtomsAdjacentPunctuation(t *testing.T) {
	reg := DefaultRegistry()
	atoms := FindInlineAtoms("ask @julian.", reg)
	if len(atoms) != 1 {
		t.Fatalf("expected 1 atom, got %d", len(atoms))
	}
	assertAtom(t, atoms[0], "@", "julian")
}

func TestFindInlineAtomsNoFalsePositiveUnregistered(t *testing.T) {
	// "%" is not registered, so %value should not match.
	reg := DefaultRegistry()
	atoms := FindInlineAtoms("use %value here", reg)
	if len(atoms) != 0 {
		t.Fatalf("expected 0 atoms for unregistered signifier, got %d", len(atoms))
	}
}

func TestFindInlineAtomsSignifierNotFollowedBySymbol(t *testing.T) {
	reg := DefaultRegistry()
	// Lone @ followed by space — no atom.
	atoms := FindInlineAtoms("email me @ noon", reg)
	if len(atoms) != 0 {
		t.Fatalf("expected 0 atoms, got %d", len(atoms))
	}
}

// --- Longest-match ---

func TestLongestMatchSignifier(t *testing.T) {
	reg := NewTokenRegistry([]string{":", "::"})
	s := classifySlot("::blocked", reg)
	if s.Token != "::" {
		t.Fatalf("expected signifier '::', got %q", s.Token)
	}
	if s.Symbol != "blocked" {
		t.Fatalf("expected symbol 'blocked', got %q", s.Symbol)
	}
}

func TestLongestMatchInlineAtom(t *testing.T) {
	reg := NewTokenRegistry([]string{":", "::"})
	atoms := FindInlineAtoms("state is ::blocked", reg)
	if len(atoms) != 1 {
		t.Fatalf("expected 1 atom, got %d", len(atoms))
	}
	assertAtom(t, atoms[0], "::", "blocked")
}

// --- helpers ---

func assertSlot(t *testing.T, s Slot, arity SlotArity, token, symbol, payload string) {
	t.Helper()
	if s.Arity != arity {
		t.Errorf("slot arity: expected %d, got %d (raw=%q)", arity, s.Arity, s.Raw)
	}
	if s.Token != token {
		t.Errorf("slot token: expected %q, got %q (raw=%q)", token, s.Token, s.Raw)
	}
	if s.Symbol != symbol {
		t.Errorf("slot symbol: expected %q, got %q (raw=%q)", symbol, s.Symbol, s.Raw)
	}
	if s.Payload != payload {
		t.Errorf("slot payload: expected %q, got %q (raw=%q)", payload, s.Payload, s.Raw)
	}
}

func TestFindPropertyListsStandaloneParagraph(t *testing.T) {
	reg := DefaultRegistry()
	pls := FindPropertyLists("(status in-progress | @julian | #research)", nil, reg)
	if len(pls) != 1 {
		t.Fatalf("expected 1 property list from standalone paragraph, got %d", len(pls))
	}
	if len(pls[0].Slots) != 3 {
		t.Fatalf("expected 3 slots, got %d", len(pls[0].Slots))
	}
	assertSlot(t, pls[0].Slots[0], ArityTwo, "status", "", "in-progress")
	assertSlot(t, pls[0].Slots[1], ArityOne, "@", "julian", "")
	assertSlot(t, pls[0].Slots[2], ArityOne, "#", "research", "")
}

func assertAtom(t *testing.T, a InlineAtom, signifier, symbol string) {
	t.Helper()
	if a.Signifier != signifier {
		t.Errorf("atom signifier: expected %q, got %q", signifier, a.Signifier)
	}
	if a.Symbol != symbol {
		t.Errorf("atom symbol: expected %q, got %q", symbol, a.Symbol)
	}
}
