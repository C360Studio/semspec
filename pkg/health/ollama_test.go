package health

import (
	"testing"
)

// goldenOllamaPs is captured `ollama ps` output from a real Apple
// Silicon host running two local models. Format pinned for the
// parser's sake: any future Ollama column rename should fail this
// test loudly rather than silently misindex.
const goldenOllamaPs = `NAME                       ID              SIZE     PROCESSOR    UNTIL
qwen3-coder:14b            abc123def456    9.0 GB   100% GPU     4 minutes from now
qwen3-coder:14b-q4         789beefcafe0    8.4 GB   100% GPU     59 seconds from now
`

func TestParseOllamaPs_Golden(t *testing.T) {
	rows := parseOllamaPs(goldenOllamaPs)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].Name != "qwen3-coder:14b" {
		t.Errorf("rows[0].Name = %q", rows[0].Name)
	}
	if rows[0].ID != "abc123def456" {
		t.Errorf("rows[0].ID = %q", rows[0].ID)
	}
	if rows[0].SizeBytes != int64(9.0*float64(1<<30)) {
		t.Errorf("rows[0].SizeBytes = %d", rows[0].SizeBytes)
	}
	// Until column rejoins the trailing tokens — the time expression
	// has spaces so naive Fields[3] would lose context.
	if rows[0].Until == "" || rows[0].Until == "GB" {
		t.Errorf("rows[0].Until = %q (expected joined time expr)", rows[0].Until)
	}
}

func TestParseOllamaPs_EmptyAndHeaderOnly(t *testing.T) {
	if rows := parseOllamaPs(""); rows != nil {
		t.Errorf("empty input: got %v", rows)
	}
	headerOnly := "NAME      ID    SIZE    PROCESSOR    UNTIL\n"
	if rows := parseOllamaPs(headerOnly); rows != nil {
		t.Errorf("header-only: got %v", rows)
	}
}

func TestParseOllamaSize(t *testing.T) {
	cases := map[string]int64{
		"":        0,
		"4.4":     0, // no unit → not a size token
		"9.0GB":   int64(9.0 * float64(1<<30)),
		"9.0":     0,
		"512MB":   512 << 20,
		"1.5TB":   int64(1.5 * float64(1<<40)),
		"garbage": 0,
		"1024K":   1024 << 10,
	}
	for tok, want := range cases {
		if got := parseOllamaSize(tok); got != want {
			t.Errorf("parseOllamaSize(%q) = %d, want %d", tok, got, want)
		}
	}
}
