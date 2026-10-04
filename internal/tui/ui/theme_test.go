package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/muesli/termenv"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	zone.NewGlobal()
	os.Exit(m.Run())
}

func TestCellTruncatesAndPads(t *testing.T) {
	if got := Cell("abcdef", 4); got != "abc…" {
		t.Fatalf("truncate: %q", got)
	}
	if got := Cell("ab", 4); got != "ab  " {
		t.Fatalf("pad: %q", got)
	}
	if got := Cell("ab", 0); got != "" {
		t.Fatalf("zero width: %q", got)
	}
}

func TestBoxFrame(t *testing.T) {
	got := Box("Title", 14, []string{"one", "a line that is far too long"})
	want := strings.Join([]string{
		"╭─ Title ────╮",
		"│ one        │",
		"│ a line th… │",
		"╰────────────╯",
	}, "\n")
	if got != want {
		t.Fatalf("box:\n%s\nwant:\n%s", got, want)
	}
}
