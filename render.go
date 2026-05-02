package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type jsonResult struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Score int    `json:"score"`
}

func renderMatches(w io.Writer, rootAbs string, matches []result, cfg config) {
	if len(matches) == 0 {
		if cfg.format == "" || cfg.format == "human" {
			fmt.Fprintln(w, "no matches")
		}
		return
	}

	switch cfg.format {
	case "plain":
		renderPlain(w, rootAbs, matches, cfg.nul)
	case "jsonl":
		renderJSONL(w, rootAbs, matches)
	default:
		renderHuman(w, rootAbs, matches, wantsColor(cfg.color))
	}
}

func renderHuman(w io.Writer, rootAbs string, matches []result, useColor bool) {
	for _, m := range matches {
		kindLabel := "<" + kindString(m.Kind) + ">"
		abs := filepath.Join(rootAbs, m.RelPath)
		style := "dir"
		if m.Kind == kindFile {
			style = classifyFileStyle(m.RelPath)
		}

		if useColor {
			abs = colorize(styleColorCode(style), abs)
			kindLabel = colorize(kindColorCode(m.Kind), kindLabel)
		}
		fmt.Fprintf(w, "%s %s\n", abs, kindLabel)
	}
}

func renderPlain(w io.Writer, rootAbs string, matches []result, nul bool) {
	sep := "\n"
	if nul {
		sep = "\x00"
	}
	for _, m := range matches {
		fmt.Fprintf(w, "%s%s", filepath.Join(rootAbs, m.RelPath), sep)
	}
}

func renderJSONL(w io.Writer, rootAbs string, matches []result) {
	enc := json.NewEncoder(w)
	for _, m := range matches {
		_ = enc.Encode(jsonResult{
			Path:  filepath.Join(rootAbs, m.RelPath),
			Kind:  kindString(m.Kind),
			Score: m.Score,
		})
	}
}

func colorize(code, s string) string {
	if code == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func kindColorCode(kind uint8) string {
	if kind == kindDir {
		return "1;36"
	}
	return "1;32"
}

func styleColorCode(style string) string {
	switch style {
	case "dir":
		return "36"
	case "go":
		return "1;32"
	case "code":
		return "34"
	case "config":
		return "35"
	case "doc":
		return "37"
	case "image":
		return "95"
	case "archive":
		return "31"
	case "media":
		return "33"
	case "exec":
		return "1;33"
	case "hidden":
		return "2;37"
	default:
		return ""
	}
}

func classifyFileStyle(path string) string {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))

	if strings.HasPrefix(base, ".") {
		return "hidden"
	}

	switch ext {
	case ".go":
		return "go"
	case ".py", ".js", ".jsx", ".ts", ".tsx", ".java", ".rb", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".php", ".cs", ".swift", ".kt", ".kts":
		return "code"
	case ".json", ".yaml", ".yml", ".toml", ".ini", ".conf", ".cfg", ".xml", ".env":
		return "config"
	case ".md", ".txt", ".rst", ".adoc":
		return "doc"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".bmp":
		return "image"
	case ".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar":
		return "archive"
	case ".mp3", ".wav", ".flac", ".ogg", ".mp4", ".mkv", ".webm", ".mov":
		return "media"
	case ".sh", ".bash", ".zsh", ".fish", ".ps1", ".exe", ".bin", ".run", ".appimage":
		return "exec"
	default:
		return "file"
	}
}
