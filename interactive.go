package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func runInteractive(cfg config, initialQuery string) error {
	if shouldUseFZF(cfg) {
		return runInteractiveFZF(cfg, initialQuery)
	}
	return runInteractivePrompt(cfg, initialQuery)
}

func runInteractivePrompt(cfg config, initialQuery string) error {
	reader := bufio.NewReader(os.Stdin)
	idx, err := prepareIndex(cfg)
	if err != nil {
		return err
	}

	fmt.Printf("interactive search mode (`q` to quit, `reload` to reindex)\n")
	fmt.Printf("root: %s\n", idx.rootAbs)
	fmt.Printf("indexed files: %d\n", len(idx.files))
	fmt.Printf("indexed dirs: %d\n", len(idx.dirs))
	if initialQuery != "" {
		matches := rankIndex(idx, initialQuery, cfg.limit, cfg.mode, cfg.progress)
		renderMatches(os.Stdout, idx.rootAbs, matches, cfg)
		fmt.Println()
	}

	for {
		fmt.Print("search> ")
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}

		line = strings.TrimSpace(line)
		if line == "q" || line == "quit" {
			return nil
		}
		if line == "reload" {
			idx, err = prepareIndex(cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			} else {
				fmt.Printf("root: %s\n", idx.rootAbs)
				fmt.Printf("reindexed files: %d\n", len(idx.files))
				fmt.Printf("reindexed dirs: %d\n", len(idx.dirs))
			}
			continue
		}

		if line != "" {
			matches := rankIndex(idx, line, cfg.limit, cfg.mode, cfg.progress)
			renderMatches(os.Stdout, idx.rootAbs, matches, cfg)
			fmt.Println()
		}

		if errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}
