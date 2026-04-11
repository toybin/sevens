package graph

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func benchmarkMarkdown(taskCount int, withInsert bool) (string, string) {
	oldLines := []string{
		"# Sevens Orthography",
		"",
		"## Today",
	}
	for i := 0; i < taskCount; i++ {
		oldLines = append(oldLines, "- [x] task "+strconv.Itoa(i)+" #sevens/identity")
	}
	oldLines = append(oldLines,
		"",
		"## Open Questions",
		"Stable block identity probably needs content anchors.",
		"",
		"## Later",
		"- [ ] persist derivation edges",
		"",
	)

	newLines := []string{
		"# Sevens Orthography",
		"",
	}
	if withInsert {
		newLines = append(newLines,
			"Intro note: block identity should survive structural edits.",
			"",
		)
	}
	newLines = append(newLines,
		"## Today",
	)
	limit := taskCount
	if limit > 0 {
		limit--
	}
	for i := 0; i < limit; i++ {
		newLines = append(newLines, "- [x] task "+strconv.Itoa(i)+" #sevens/identity")
	}
	newLines = append(newLines,
		"",
		"## Decisions",
		"Stable block identity probably needs deterministic content anchors.",
		"",
		"## Blocked",
	)
	if taskCount > 0 {
		newLines = append(newLines, "- [!!] task "+strconv.Itoa(taskCount-1)+" #sevens/identity")
	}
	newLines = append(newLines,
		"",
		"## Later",
		"- [ ] persist derivation edges",
		"",
	)

	return strings.Join(oldLines, "\n"), strings.Join(newLines, "\n")
}

func BenchmarkDiffParsedBlocks_ShiftedDocument_200(b *testing.B) {
	oldDoc, newDoc := benchmarkMarkdown(200, true)
	oldBlocks := extractBlocks(oldDoc)
	newBlocks := extractBlocks(newDoc)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DiffParsedBlocks(oldBlocks, newBlocks)
	}
}

func BenchmarkDiffParsedBlocks_ShiftedDocument_1000(b *testing.B) {
	oldDoc, newDoc := benchmarkMarkdown(1000, true)
	oldBlocks := extractBlocks(oldDoc)
	newBlocks := extractBlocks(newDoc)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DiffParsedBlocks(oldBlocks, newBlocks)
	}
}

func BenchmarkExternalDiffUnified_ShiftedDocument_200(b *testing.B) {
	oldDoc, newDoc := benchmarkMarkdown(200, true)
	dir := b.TempDir()
	oldPath := filepath.Join(dir, "old.md")
	newPath := filepath.Join(dir, "new.md")
	if err := os.WriteFile(oldPath, []byte(oldDoc), 0644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(newDoc), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd := exec.Command("/usr/bin/diff", "-u", oldPath, newPath)
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				continue
			}
			b.Fatal(err)
		}
	}
}

func BenchmarkExternalDiffUnified_ShiftedDocument_1000(b *testing.B) {
	oldDoc, newDoc := benchmarkMarkdown(1000, true)
	dir := b.TempDir()
	oldPath := filepath.Join(dir, "old.md")
	newPath := filepath.Join(dir, "new.md")
	if err := os.WriteFile(oldPath, []byte(oldDoc), 0644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(newDoc), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd := exec.Command("/usr/bin/diff", "-u", oldPath, newPath)
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				continue
			}
			b.Fatal(err)
		}
	}
}
