package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunRGPathListAllowsContentNoMatches(t *testing.T) {
	binDir := t.TempDir()
	root := t.TempDir()
	writeFakeRG(t, binDir, 1, "", "")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	files, err := listFilesByContentRG(config{root: root, text: "needle"})
	if err != nil {
		t.Fatalf("listFilesByContentRG returned error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("len(files) = %d, want 0", len(files))
	}
}

func TestRunRGPathListKeepsRealErrors(t *testing.T) {
	binDir := t.TempDir()
	root := t.TempDir()
	writeFakeRG(t, binDir, 1, "", "bad regex")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := listFilesByContentRG(config{root: root, text: "["})
	if err == nil {
		t.Fatalf("expected rg stderr to become an error")
	}
}

func TestWalkPathsHonorsGlobHiddenAndIgnore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gitignore"), "ignored/\n")
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "b.go"), "b")
	writeFile(t, filepath.Join(root, ".hidden.txt"), "hidden")
	writeFile(t, filepath.Join(root, "sub", "c.txt"), "c")
	writeFile(t, filepath.Join(root, "ignored", "skip.txt"), "skip")

	files, _, err := walkPaths(config{root: root, glob: "*.txt"}, true, false)
	if err != nil {
		t.Fatalf("walkPaths returned error: %v", err)
	}
	want := []string{"a.txt", filepath.Join("sub", "c.txt")}
	if !sameStrings(files, want) {
		t.Fatalf("files = %#v, want %#v", files, want)
	}
}

func TestWalkPathsIncludesEmptyDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, dirs, err := walkPaths(config{root: root}, false, true)
	if err != nil {
		t.Fatalf("walkPaths returned error: %v", err)
	}
	if !sameStrings(dirs, []string{"empty"}) {
		t.Fatalf("dirs = %#v, want [empty]", dirs)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFakeRG(t *testing.T, dir string, code int, stdout, stderr string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		body := "@echo off\r\n"
		if stdout != "" {
			body += "echo " + stdout + "\r\n"
		}
		if stderr != "" {
			body += "echo " + stderr + " 1>&2\r\n"
		}
		body += "exit /b " + string(rune('0'+code)) + "\r\n"
		writeFile(t, filepath.Join(dir, "rg.bat"), body)
		return
	}

	body := "#!/bin/sh\n"
	if stdout != "" {
		body += "printf '%s\\n' " + shellQuote(stdout) + "\n"
	}
	if stderr != "" {
		body += "printf '%s\\n' " + shellQuote(stderr) + " >&2\n"
	}
	body += "exit " + string(rune('0'+code)) + "\n"
	path := filepath.Join(dir, "rg")
	writeFile(t, path, body)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
