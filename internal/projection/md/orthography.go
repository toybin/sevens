package md

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// SlotArity classifies a property list slot.
type SlotArity int

const (
	ArityZero SlotArity = iota // bare token: "x", "!", "::blocked"
	ArityOne                   // signifier-symbol fused: "@julian", "#research"
	ArityTwo                   // key + payload: "status done", "@ julian"
)

// Slot is one entry in a property list.
type Slot struct {
	Arity   SlotArity
	Token   string // the token head: "x", "@", "status", "!", "::"
	Symbol  string // for arity-1: the fused symbol part ("julian" from "@julian")
	Payload string // for arity-2: the payload after whitespace
	Raw     string // the original slot text, trimmed
}

// PropertyList is an attached property list parsed from a block.
type PropertyList struct {
	Slots     []Slot
	BlockPath string // which block this is attached to
	Raw       string // original text including parens
}

// InlineAtom is a signifier-symbol atom found in body text.
type InlineAtom struct {
	Signifier string // "@", "#", "::", etc.
	Symbol    string // "julian", "research", "blocked"
	Offset    int    // byte offset in the source text
}

// TokenRegistry holds configured signifiers for parsing.
type TokenRegistry struct {
	Signifiers []string // registered signifier tokens, sorted longest-first
}

// NewTokenRegistry creates a registry from a list of signifier strings.
// Sorts them longest-first for longest-match parsing.
func NewTokenRegistry(signifiers []string) *TokenRegistry {
	sorted := make([]string, len(signifiers))
	copy(sorted, signifiers)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i]) > len(sorted[j])
	})
	return &TokenRegistry{Signifiers: sorted}
}

// DefaultRegistry returns the default signifier registry.
func DefaultRegistry() *TokenRegistry {
	return NewTokenRegistry([]string{"#", "@", "!", "~", "::"})
}

// matchSignifier checks if text starts with a registered signifier.
// Returns the matched signifier or "" if none match (longest-first).
func (r *TokenRegistry) matchSignifier(text string) string {
	for _, sig := range r.Signifiers {
		if strings.HasPrefix(text, sig) {
			return sig
		}
	}
	return ""
}

// isSignifier returns true if the token exactly matches a registered signifier.
func (r *TokenRegistry) isSignifier(token string) bool {
	for _, sig := range r.Signifiers {
		if token == sig {
			return true
		}
	}
	return false
}

// classifySlot determines the arity and parses the components of a raw slot string.
func classifySlot(raw string, reg *TokenRegistry) Slot {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Slot{Arity: ArityZero, Raw: trimmed}
	}

	// Check if it starts with a registered signifier.
	sig := reg.matchSignifier(trimmed)

	if sig != "" {
		rest := trimmed[len(sig):]

		// Arity-1: signifier immediately followed by non-whitespace symbol.
		// e.g. "@julian", "#research", "::blocked"
		if len(rest) > 0 && !unicode.IsSpace(rune(rest[0])) {
			// The symbol is everything up to the end (no whitespace in symbol).
			symbol := rest
			return Slot{
				Arity:  ArityOne,
				Token:  sig,
				Symbol: symbol,
				Raw:    trimmed,
			}
		}

		// Arity-2: signifier followed by whitespace then payload.
		// e.g. "@ julian", ":: state-name"
		if len(rest) > 0 {
			payload := strings.TrimSpace(rest)
			if payload != "" {
				return Slot{
					Arity:   ArityTwo,
					Token:   sig,
					Payload: payload,
					Raw:     trimmed,
				}
			}
		}

		// Arity-0: bare signifier with nothing after it.
		// e.g. "!"
		return Slot{
			Arity: ArityZero,
			Token: sig,
			Raw:   trimmed,
		}
	}

	// No signifier prefix. Check for word-key + whitespace + payload (arity-2).
	idx := strings.IndexFunc(trimmed, unicode.IsSpace)
	if idx > 0 {
		key := trimmed[:idx]
		payload := strings.TrimSpace(trimmed[idx:])
		if payload != "" {
			return Slot{
				Arity:   ArityTwo,
				Token:   key,
				Payload: payload,
				Raw:     trimmed,
			}
		}
	}

	// Arity-0: bare token.
	return Slot{
		Arity: ArityZero,
		Token: trimmed,
		Raw:   trimmed,
	}
}

// ParsePropertyList parses the content of a property list (between parens).
// Input is the text between ( and ), which may span multiple lines.
// Returns the parsed slots.
func ParsePropertyList(content string, reg *TokenRegistry) []Slot {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	// Normalize multiline: replace newline-leading-pipe sequences with |.
	// A line that starts with optional whitespace + "|" is a slot separator.
	lines := strings.Split(content, "\n")
	var normalized strings.Builder
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 {
			normalized.WriteString(trimmed)
			continue
		}
		if strings.HasPrefix(trimmed, "|") {
			// This is a continuation separator line.
			normalized.WriteString(" | ")
			rest := strings.TrimSpace(trimmed[1:])
			normalized.WriteString(rest)
		} else if trimmed != "" {
			normalized.WriteString(" ")
			normalized.WriteString(trimmed)
		}
	}

	// Split on | to get individual slot texts.
	parts := strings.Split(normalized.String(), "|")
	var slots []Slot
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		slots = append(slots, classifySlot(trimmed, reg))
	}
	return slots
}

// propertyListRe matches a parenthesized property list, possibly multiline.
// It captures the content between the outermost parens.
var propertyListRe = regexp.MustCompile(`\(([^)]*)\)`)

// headingLineRe matches an ATX heading line.
var headingLineRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

// listItemLineRe matches an unordered or ordered list item line.
var listItemLineRe = regexp.MustCompile(`^(\s*[-*+]|\s*\d+\.)\s+(.*)$`)

// isAttachmentLine returns true if the line is a heading or list item.
func isAttachmentLine(line string) (kind string, ok bool) {
	if headingLineRe.MatchString(line) {
		return "heading", true
	}
	if listItemLineRe.MatchString(line) {
		return "list-item", true
	}
	return "", false
}

// isStandalonePropertyList checks if a line is ONLY a property list (possibly
// with leading whitespace). Used for following-line attachment.
func isStandalonePropertyList(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "(") || !strings.HasSuffix(trimmed, ")") {
		return false
	}
	// Verify the parens balance — the first ( should match the last ).
	depth := 0
	for i, ch := range trimmed {
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 && i < len(trimmed)-1 {
				return false // closing paren isn't the last char
			}
		}
	}
	return depth == 0
}

// FindPropertyLists finds attached property lists on a block line and its
// continuation lines. Returns found property lists with their raw text.
func FindPropertyLists(blockText string, continuationLines []string, reg *TokenRegistry) []PropertyList {
	var result []PropertyList

	blockLine := blockText
	kind, isBlock := isAttachmentLine(blockLine)
	if !isBlock {
		// Check if the entire line is a standalone property list (paragraph case).
		trimmed := strings.TrimSpace(blockLine)
		if isStandalonePropertyList(trimmed) {
			content := trimmed[1 : len(trimmed)-1]
			slots := ParsePropertyList(content, reg)
			result = append(result, PropertyList{
				Slots: slots,
				Raw:   trimmed,
			})
		}
		return result
	}

	// For headings and list items, look for inline property lists.
	// Extract the text portion (after the heading marker or list marker).
	var textPortion string
	switch kind {
	case "heading":
		m := headingLineRe.FindStringSubmatch(blockLine)
		if m != nil {
			textPortion = m[2]
		}
	case "list-item":
		m := listItemLineRe.FindStringSubmatch(blockLine)
		if m != nil {
			textPortion = m[2]
		}
	}

	// Find all property lists in the text portion.
	matches := propertyListRe.FindAllStringSubmatch(textPortion, -1)
	for _, m := range matches {
		raw := m[0]
		content := m[1]
		slots := ParsePropertyList(content, reg)
		result = append(result, PropertyList{
			Slots: slots,
			Raw:   raw,
		})
	}

	// Check continuation lines for standalone property lists.
	// For multiline property lists, we need to handle the case where
	// "(" is on one line and ")" is on a later line.
	for i := 0; i < len(continuationLines); i++ {
		line := continuationLines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		// Single-line standalone property list.
		if isStandalonePropertyList(trimmed) {
			content := trimmed[1 : len(trimmed)-1]
			slots := ParsePropertyList(content, reg)
			result = append(result, PropertyList{
				Slots: slots,
				Raw:   trimmed,
			})
			continue
		}

		// Multiline property list: starts with "(" but doesn't close on this line.
		if strings.HasPrefix(trimmed, "(") && !strings.Contains(trimmed, ")") {
			var rawBuilder strings.Builder
			var contentBuilder strings.Builder
			rawBuilder.WriteString(trimmed)
			// Content is everything after the opening paren on this line.
			contentBuilder.WriteString(strings.TrimSpace(trimmed[1:]))

			// Consume lines until we find the closing ")".
			closed := false
			for i++; i < len(continuationLines); i++ {
				contLine := continuationLines[i]
				contTrimmed := strings.TrimSpace(contLine)
				rawBuilder.WriteString("\n")
				rawBuilder.WriteString(contLine)

				if strings.HasSuffix(contTrimmed, ")") {
					// Last line of the multiline property list.
					contentBuilder.WriteString("\n")
					contentBuilder.WriteString(contTrimmed[:len(contTrimmed)-1])
					closed = true
					break
				}
				contentBuilder.WriteString("\n")
				contentBuilder.WriteString(contTrimmed)
			}

			if closed {
				slots := ParsePropertyList(contentBuilder.String(), reg)
				result = append(result, PropertyList{
					Slots: slots,
					Raw:   rawBuilder.String(),
				})
			}
		}
	}

	return result
}

// symbolCharRe matches valid symbol characters (not whitespace, not punctuation
// that commonly terminates a symbol).
func isSymbolChar(r rune) bool {
	if unicode.IsSpace(r) {
		return false
	}
	// Terminate on common punctuation that follows atoms in prose.
	switch r {
	case '.', ',', ';', ':', '!', '?', ')', '(', '[', ']', '{', '}', '"', '\'':
		return false
	}
	return true
}

// FindInlineAtoms finds signifier-symbol atoms in body text.
// Respects code spans (backticks) and doesn't match inside them.
func FindInlineAtoms(text string, reg *TokenRegistry) []InlineAtom {
	var atoms []InlineAtom

	// Build a set of byte ranges that are inside code spans.
	shielded := codeSpanRanges(text)

	// Scan through text looking for signifiers.
	i := 0
	for i < len(text) {
		// Skip if we're inside a code span.
		if isShielded(i, shielded) {
			i++
			continue
		}

		sig := reg.matchSignifier(text[i:])
		if sig == "" {
			i++
			continue
		}

		sigEnd := i + len(sig)
		if sigEnd >= len(text) {
			i++
			continue
		}

		// The signifier must be immediately followed by a non-whitespace
		// symbol character (arity-1 form).
		nextRune := rune(text[sigEnd])
		if unicode.IsSpace(nextRune) || !isSymbolChar(nextRune) {
			i++
			continue
		}

		// Collect symbol characters.
		symEnd := sigEnd
		for symEnd < len(text) && isSymbolChar(rune(text[symEnd])) {
			symEnd++
		}

		symbol := text[sigEnd:symEnd]
		if symbol == "" {
			i++
			continue
		}

		atoms = append(atoms, InlineAtom{
			Signifier: sig,
			Symbol:    symbol,
			Offset:    i,
		})

		i = symEnd
	}

	return atoms
}

// codeSpanRange is a byte range [start, end) inside a code span.
type codeSpanRange struct {
	start, end int
}

// codeSpanRanges finds all backtick-delimited code spans in text.
func codeSpanRanges(text string) []codeSpanRange {
	var ranges []codeSpanRange
	i := 0
	for i < len(text) {
		if text[i] != '`' {
			i++
			continue
		}
		// Count opening backticks.
		tickStart := i
		tickLen := 0
		for i < len(text) && text[i] == '`' {
			tickLen++
			i++
		}
		// Find matching closing backticks.
		closer := strings.Repeat("`", tickLen)
		closeIdx := strings.Index(text[i:], closer)
		if closeIdx == -1 {
			break
		}
		spanEnd := i + closeIdx + tickLen
		ranges = append(ranges, codeSpanRange{tickStart, spanEnd})
		i = spanEnd
	}
	return ranges
}

// isShielded returns true if byte offset pos falls inside any code span range.
func isShielded(pos int, ranges []codeSpanRange) bool {
	for _, r := range ranges {
		if pos >= r.start && pos < r.end {
			return true
		}
	}
	return false
}
