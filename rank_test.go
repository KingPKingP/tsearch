package main

import "testing"

func TestWildcardMatch(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{pattern: "*.txt", value: "notes.txt", want: true},
		{pattern: "src/*/main.go", value: "src/app/main.go", want: true},
		{pattern: "src/?/main.go", value: "src/app/main.go", want: false},
		{pattern: "*.md", value: "notes.txt", want: false},
	}

	for _, tt := range tests {
		if got := wildcardMatch(tt.pattern, tt.value); got != tt.want {
			t.Fatalf("wildcardMatch(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
		}
	}
}

func TestFuzzyScorePrefersBoundaries(t *testing.T) {
	boundaryScore, boundaryOK := fuzzyScore("test", "src/foo/test-data/config.go")
	embeddedScore, embeddedOK := fuzzyScore("test", "src/foo/contestdata/config.go")
	if !boundaryOK || !embeddedOK {
		t.Fatalf("expected both paths to match")
	}
	if boundaryScore <= embeddedScore {
		t.Fatalf("boundary score %d should be greater than embedded score %d", boundaryScore, embeddedScore)
	}
}

func TestRankIndexUsesExplicitDirs(t *testing.T) {
	idx := index{dirs: []string{"empty-dir"}}
	matches := rankIndex(idx, "empty", 10, "dir", false)
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if matches[0].Kind != kindDir || matches[0].RelPath != "empty-dir" {
		t.Fatalf("match = %#v, want empty-dir dir", matches[0])
	}
}
