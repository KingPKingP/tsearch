package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func resolveEditor(cfg config) ([]string, error) {
	editor := strings.TrimSpace(cfg.editor)
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("VISUAL"))
	}
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		for _, candidate := range []string{"nvim", "vim", "vi", "nano", "code"} {
			if _, err := exec.LookPath(candidate); err == nil {
				editor = candidate
				break
			}
		}
	}
	if editor == "" {
		return nil, errors.New("no editor configured (set $EDITOR or use -editor)")
	}

	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return nil, errors.New("invalid editor command")
	}
	return parts, nil
}

func openPathInEditor(cfg config, path string) error {
	cmdParts, err := resolveEditor(cfg)
	if err != nil {
		return err
	}
	bin, err := exec.LookPath(cmdParts[0])
	if err != nil {
		return fmt.Errorf("editor %q not found in PATH", cmdParts[0])
	}

	args := append(cmdParts[1:], path)
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func copyPathToClipboard(path string) error {
	type clipCmd struct {
		name string
		args []string
	}
	commands := []clipCmd{
		{name: "pbcopy"},
		{name: "wl-copy"},
		{name: "xclip", args: []string{"-selection", "clipboard"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
		{name: "clip.exe"},
		{name: "clip"},
	}

	var lastErr error
	for _, c := range commands {
		bin, err := exec.LookPath(c.name)
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, c.args...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			lastErr = err
			continue
		}
		if err := cmd.Start(); err != nil {
			lastErr = err
			_ = stdin.Close()
			continue
		}
		if _, err := io.WriteString(stdin, path); err != nil {
			lastErr = err
		}
		_ = stdin.Close()
		if err := cmd.Wait(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("no clipboard tool found (tried pbcopy, wl-copy, xclip, xsel, clip)")
}

func wantsColor(mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	default:
		return fileIsTTY(os.Stdout)
	}
}

func fileIsTTY(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
