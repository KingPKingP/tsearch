package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type fzfEvent struct {
	Query string
	Key   string
	Path  string
	Kind  uint8
	OK    bool
}

func runInteractiveFZF(cfg config, initialQuery string) error {
	idx, err := prepareIndex(cfg)
	if err != nil {
		return err
	}

	query := strings.TrimSpace(initialQuery)
	for {
		event, err := runFZFOnce(cfg, idx, query)
		if err != nil {
			return err
		}

		query = event.Query
		switch event.Key {
		case "ctrl-r":
			idx, err = prepareIndex(cfg)
			if err != nil {
				return err
			}
			continue
		case "ctrl-y":
			if event.OK {
				if err := copyPathToClipboard(event.Path); err != nil {
					fmt.Fprintf(os.Stderr, "copy failed: %v\n", err)
				} else {
					fmt.Fprintf(os.Stderr, "copied: %s\n", event.Path)
				}
			}
			continue
		case "enter", "ctrl-o", "":
			if !event.OK {
				return nil
			}
			if err := openPathInEditor(cfg, event.Path); err != nil {
				return err
			}
			continue
		default:
			return nil
		}
	}
}

func runFZFOnce(cfg config, idx index, query string) (fzfEvent, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return fzfEvent{}, errors.New("fzf not found; install fzf or run -ui plain")
	}

	args := []string{
		"--layout=reverse",
		"--height=95%",
		"--border",
		"--prompt=tsearch> ",
		"--delimiter=\t",
		"--with-nth=2,3",
		"--expect=enter,ctrl-o,ctrl-y,ctrl-r",
		"--print-query",
		"--header=enter/ctrl-o=open  ctrl-y=copy path  ctrl-r=reindex  esc=quit",
	}
	if query != "" {
		args = append(args, "--query", query)
	}

	cmd := exec.Command("fzf", args...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fzfEvent{}, err
	}

	go func() {
		w := bufio.NewWriterSize(stdin, 1<<20)
		writeFZFItems(w, cfg, idx)
		_ = w.Flush()
		_ = stdin.Close()
	}()

	out, err := cmd.Output()
	if err != nil {
		var e *exec.ExitError
		if errors.As(err, &e) {
			// fzf: 130 = ESC/Ctrl-C, 1 = no selection.
			if e.ExitCode() == 130 || e.ExitCode() == 1 {
				return fzfEvent{}, nil
			}
		}
		return fzfEvent{}, err
	}

	return parseFZFOutput(out), nil
}

func writeFZFItems(w *bufio.Writer, cfg config, idx index) {
	if cfg.mode == "file" || cfg.mode == "all" {
		for _, rel := range idx.files {
			abs := filepath.Join(idx.rootAbs, rel)
			fmt.Fprintf(w, "%d\tFILE\t%s\n", kindFile, abs)
		}
	}
	if cfg.mode == "dir" || cfg.mode == "all" {
		for _, rel := range idx.dirs {
			abs := filepath.Join(idx.rootAbs, rel)
			fmt.Fprintf(w, "%d\tDIR\t%s\n", kindDir, abs)
		}
	}
}

func parseFZFOutput(out []byte) fzfEvent {
	event := fzfEvent{}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 0 {
		return event
	}

	event.Query = strings.TrimSpace(lines[0])
	if len(lines) >= 2 {
		event.Key = strings.TrimSpace(lines[1])
	}
	if len(lines) < 3 {
		return event
	}

	fields := strings.SplitN(lines[2], "\t", 3)
	if len(fields) < 3 {
		return event
	}

	event.OK = true
	event.Path = strings.TrimSpace(fields[2])
	if strings.TrimSpace(fields[0]) == "0" {
		event.Kind = kindDir
	} else {
		event.Kind = kindFile
	}
	if event.Key == "" {
		event.Key = "enter"
	}
	return event
}

func shouldUseFZF(cfg config) bool {
	if cfg.ui == "plain" {
		return false
	}
	if cfg.ui == "fzf" {
		return true
	}
	if !fileIsTTY(os.Stdin) || !fileIsTTY(os.Stdout) {
		return false
	}
	_, err := exec.LookPath("fzf")
	return err == nil
}
