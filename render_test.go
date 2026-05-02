package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestRenderPlainAndNUL(t *testing.T) {
	root := t.TempDir()
	matches := []result{{RelPath: "a.txt", Kind: kindFile, Score: 10}}

	var plain bytes.Buffer
	renderMatches(&plain, root, matches, config{format: "plain"})
	if got, want := plain.String(), filepath.Join(root, "a.txt")+"\n"; got != want {
		t.Fatalf("plain output = %q, want %q", got, want)
	}

	var nul bytes.Buffer
	renderMatches(&nul, root, matches, config{format: "plain", nul: true})
	if got, want := nul.String(), filepath.Join(root, "a.txt")+"\x00"; got != want {
		t.Fatalf("nul output = %q, want %q", got, want)
	}
}

func TestRenderJSONL(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	renderMatches(&out, root, []result{{RelPath: "a.txt", Kind: kindFile, Score: 10}}, config{format: "jsonl"})

	var got jsonResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json output did not decode: %v", err)
	}
	if got.Path != filepath.Join(root, "a.txt") || got.Kind != "file" || got.Score != 10 {
		t.Fatalf("json result = %#v", got)
	}
}

func TestRenderNoMatchesPlainIsSilent(t *testing.T) {
	var out bytes.Buffer
	renderMatches(&out, t.TempDir(), nil, config{format: "plain"})
	if out.Len() != 0 {
		t.Fatalf("plain no-match output = %q, want empty", out.String())
	}
}
