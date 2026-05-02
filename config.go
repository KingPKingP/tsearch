package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

type config struct {
	root        string
	mode        string
	limit       int
	hidden      bool
	noIgnore    bool
	color       string
	interactive bool
	text        string
	glob        string
	progress    bool
	once        bool
	ui          string
	editor      string
	format      string
	nul         bool
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.root, "root", ".", "Root directory to search")
	flag.StringVar(&cfg.mode, "type", "all", "Search type: file|dir|all")
	flag.IntVar(&cfg.limit, "limit", 100, "Max number of results")
	flag.BoolVar(&cfg.hidden, "hidden", false, "Include hidden files")
	flag.BoolVar(&cfg.noIgnore, "no-ignore", false, "Ignore .gitignore and other ignore rules")
	flag.StringVar(&cfg.color, "color", "auto", "Color mode: auto|always|never")
	flag.BoolVar(&cfg.interactive, "i", false, "Force interactive mode")
	flag.StringVar(&cfg.text, "text", "", "Search text inside files (content search)")
	flag.StringVar(&cfg.glob, "glob", "", "Limit file paths using glob pattern (e.g. '*.txt')")
	flag.BoolVar(&cfg.progress, "progress", true, "Show progress for large scans/matches")
	flag.BoolVar(&cfg.once, "once", false, "Run a single query and exit")
	flag.StringVar(&cfg.ui, "ui", "auto", "Interactive UI: auto|plain|fzf")
	flag.StringVar(&cfg.editor, "editor", "", "Editor command for opening selections (default: $VISUAL/$EDITOR)")
	flag.StringVar(&cfg.format, "format", "human", "Output format: human|plain|jsonl")
	flag.BoolVar(&cfg.nul, "0", false, "Use NUL-separated paths (plain output)")
	flag.Usage = usage
	flag.Parse()

	cfg.mode = strings.ToLower(strings.TrimSpace(cfg.mode))
	cfg.color = strings.ToLower(strings.TrimSpace(cfg.color))
	cfg.text = strings.TrimSpace(cfg.text)
	cfg.glob = strings.TrimSpace(cfg.glob)
	cfg.ui = strings.ToLower(strings.TrimSpace(cfg.ui))
	cfg.editor = strings.TrimSpace(cfg.editor)
	cfg.format = strings.ToLower(strings.TrimSpace(cfg.format))

	if cfg.limit <= 0 {
		cfg.limit = 100
	}

	if cfg.mode != "file" && cfg.mode != "dir" && cfg.mode != "all" {
		fatal(fmt.Errorf("invalid -type %q (expected file|dir|all)", cfg.mode))
	}
	if cfg.color != "auto" && cfg.color != "always" && cfg.color != "never" {
		fatal(fmt.Errorf("invalid -color %q (expected auto|always|never)", cfg.color))
	}
	if cfg.ui != "auto" && cfg.ui != "plain" && cfg.ui != "fzf" {
		fatal(fmt.Errorf("invalid -ui %q (expected auto|plain|fzf)", cfg.ui))
	}
	if cfg.format != "human" && cfg.format != "plain" && cfg.format != "jsonl" {
		fatal(fmt.Errorf("invalid -format %q (expected human|plain|jsonl)", cfg.format))
	}
	if cfg.nul {
		if cfg.format == "human" {
			cfg.format = "plain"
		}
		if cfg.format != "plain" {
			fatal(errors.New("-0 can only be used with -format plain"))
		}
	}
	return cfg
}

func usage() {
	fmt.Fprintf(os.Stderr, `tsearch: fast file + directory finder

Usage:
  tsearch [flags]
  tsearch [flags] <query>

Notes:
  - Starts interactive mode by default.
  - If a query is provided, it runs that query first, then stays interactive.
  - Use -once to run a single query and exit.
  - Use -text for file content search (optionally with -glob '*.txt').

Flags:
`)
	flag.PrintDefaults()
}
