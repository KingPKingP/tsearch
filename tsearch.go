package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	cfg := parseFlags()
	query := strings.TrimSpace(strings.Join(flag.Args(), " "))

	if cfg.text != "" {
		if cfg.interactive {
			fatal(errors.New("-i is not supported together with -text"))
		}
		if err := runContentSearch(os.Stdout, cfg, query); err != nil {
			fatal(err)
		}
		return
	}

	if cfg.interactive || query == "" || !cfg.once {
		if err := runInteractive(cfg, query); err != nil {
			fatal(err)
		}
		return
	}

	if err := runSearch(os.Stdout, cfg, query); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
