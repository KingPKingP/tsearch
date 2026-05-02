package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
)

type index struct {
	rootAbs string
	files   []string
	dirs    []string
}

type ignoreFrame struct {
	dir   string
	rules []ignoreRule
}

type ignoreRule struct {
	pattern  string
	negated  bool
	dirOnly  bool
	anchored bool
}

func listPaths(cfg config) ([]string, []string, error) {
	if hasRG() {
		files, err := listFilesWithRG(cfg)
		if err != nil {
			return nil, nil, err
		}
		dirs := deriveDirs(files)
		if cfg.glob == "" && (cfg.mode == "dir" || cfg.mode == "all") {
			walkedDirs, err := listDirsWithWalk(cfg)
			if err != nil && len(dirs) == 0 {
				return nil, nil, err
			}
			if err == nil {
				dirs = mergeDirs(dirs, walkedDirs)
			}
		}
		return files, dirs, nil
	}
	return walkPaths(cfg, true, cfg.mode == "dir" || cfg.mode == "all")
}

func hasRG() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}

func listFilesWithRG(cfg config) ([]string, error) {
	args := []string{"--files", "--no-messages"}
	if cfg.hidden {
		args = append(args, "--hidden")
	}
	if cfg.noIgnore {
		args = append(args, "--no-ignore")
	}
	if cfg.glob != "" {
		args = append(args, "--glob", cfg.glob)
	}
	return runRGPathList(cfg, args, "indexing", false)
}

func listFilesByContentRG(cfg config) ([]string, error) {
	if !hasRG() {
		return nil, errors.New("-text requires rg in PATH")
	}

	args := []string{"-l", "--no-messages", "--color", "never"}
	if cfg.hidden {
		args = append(args, "--hidden")
	}
	if cfg.noIgnore {
		args = append(args, "--no-ignore")
	}
	if cfg.glob != "" {
		args = append(args, "--glob", cfg.glob)
	}

	args = append(args, "-e", cfg.text, ".")
	return runRGPathList(cfg, args, "content-scan", true)
}

func runRGPathList(cfg config, args []string, label string, emptyExitOK bool) ([]string, error) {
	cmd := exec.Command("rg", args...)
	cmd.Dir = cfg.root

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var stderrBuf bytes.Buffer
	errDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderrBuf, stderrPipe)
		close(errDone)
	}()

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	results := make([]string, 0, 8192)

	meter := newProgressMeter(label, 0, cfg.progress)
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		clean := filepath.Clean(line)
		if clean == "." {
			continue
		}
		results = append(results, clean)
		count++
		if count > 0 && count%512 == 0 {
			meter.TickUnknown(count)
		}
	}
	meter.TickUnknown(count)
	meter.FinishUnknown(count)

	if scanErr := scanner.Err(); scanErr != nil {
		_ = cmd.Wait()
		<-errDone
		return nil, scanErr
	}

	waitErr := cmd.Wait()
	<-errDone
	if waitErr != nil {
		var exitErr *exec.ExitError
		stderrText := strings.TrimSpace(stderrBuf.String())
		if emptyExitOK && len(results) == 0 && stderrText == "" && errors.As(waitErr, &exitErr) && exitErr.ExitCode() == 1 {
			return results, nil
		}
		// Permission-denied paths can still produce valid partial output.
		if len(results) > 0 {
			return results, nil
		}
		if stderrText != "" {
			return nil, fmt.Errorf("rg failed: %s", stderrText)
		}
		return nil, fmt.Errorf("rg failed: %w", waitErr)
	}
	return results, nil
}

func listDirsWithWalk(cfg config) ([]string, error) {
	_, dirs, err := walkPaths(cfg, false, true)
	return dirs, err
}

func walkPaths(cfg config, wantFiles, wantDirs bool) ([]string, []string, error) {
	rootAbs, err := filepath.Abs(cfg.root)
	if err != nil {
		return nil, nil, err
	}

	files := make([]string, 0, 8192)
	dirs := make([]string, 0, 1024)
	meter := newProgressMeter("indexing", 0, cfg.progress)
	count := 0
	frames := make([]ignoreFrame, 0, 8)

	err = filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Ignore inaccessible paths to keep whole-system scans moving.
			return nil
		}

		frames = trimIgnoreFrames(frames, path)
		if path == rootAbs {
			if !cfg.noIgnore {
				frames = append(frames, ignoreFrame{dir: path, rules: readIgnoreRules(path)})
			}
			return nil
		}

		if !cfg.hidden && isHiddenName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !cfg.noIgnore && isIgnoredByFrames(path, d, frames) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, relErr := filepath.Rel(rootAbs, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.Clean(rel)
		if rel == "." {
			return nil
		}

		if d.IsDir() {
			if wantDirs && (cfg.glob == "" || pathMatchesGlob(cfg.glob, rel)) {
				dirs = append(dirs, rel)
				count++
			}
			if !cfg.noIgnore {
				frames = append(frames, ignoreFrame{dir: path, rules: readIgnoreRules(path)})
			}
		} else if wantFiles && pathMatchesGlob(cfg.glob, rel) {
			files = append(files, rel)
			count++
		}

		if count%512 == 0 {
			meter.TickUnknown(count)
		}
		return nil
	})

	sort.Strings(files)
	sort.Strings(dirs)
	meter.TickUnknown(count)
	meter.FinishUnknown(count)
	return files, dirs, err
}

func deriveDirs(files []string) []string {
	seen := map[string]struct{}{}
	for _, rel := range files {
		dir := filepath.Dir(rel)
		for dir != "." && dir != string(filepath.Separator) && dir != "" {
			seen[dir] = struct{}{}
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
			dir = next
		}
	}
	return sortedKeys(seen)
}

func mergeDirs(a, b []string) []string {
	seen := map[string]struct{}{}
	for _, dir := range a {
		if dir != "." && dir != "" {
			seen[dir] = struct{}{}
		}
	}
	for _, dir := range b {
		if dir != "." && dir != "" {
			seen[dir] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func pathMatchesGlob(glob, rel string) bool {
	if glob == "" {
		return true
	}

	slashRel := filepath.ToSlash(rel)
	slashGlob := filepath.ToSlash(glob)
	base := pathpkg.Base(slashRel)

	if ok, err := pathpkg.Match(slashGlob, slashRel); err == nil && ok {
		return true
	}
	if ok, err := pathpkg.Match(slashGlob, base); err == nil && ok {
		return true
	}
	return slashGlob == slashRel || slashGlob == base
}

func isHiddenName(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

func trimIgnoreFrames(frames []ignoreFrame, path string) []ignoreFrame {
	kept := frames[:0]
	for _, frame := range frames {
		if pathWithin(path, frame.dir) {
			kept = append(kept, frame)
		}
	}
	return kept
}

func pathWithin(path, dir string) bool {
	if path == dir {
		return true
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func readIgnoreRules(dir string) []ignoreRule {
	file, err := os.Open(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	defer file.Close()

	rules := make([]ignoreRule, 0, 16)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, `\#`) {
			line = line[1:]
		}

		rule := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			rule.negated = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		}
		if strings.HasPrefix(line, "/") {
			rule.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rule.pattern = filepath.ToSlash(line)
		rules = append(rules, rule)
	}
	return rules
}

func isIgnoredByFrames(path string, d fs.DirEntry, frames []ignoreFrame) bool {
	ignored := false
	for _, frame := range frames {
		rel, err := filepath.Rel(frame.dir, path)
		if err != nil || rel == "." {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, rule := range frame.rules {
			if ignoreRuleMatches(rule, rel, d.Name(), d.IsDir()) {
				ignored = !rule.negated
			}
		}
	}
	return ignored
}

func ignoreRuleMatches(rule ignoreRule, rel, name string, isDir bool) bool {
	if rule.dirOnly && !isDir {
		return false
	}

	pattern := rule.pattern
	if rule.anchored || strings.Contains(pattern, "/") {
		if matchSlashPattern(pattern, rel) {
			return true
		}
		return rule.dirOnly && (rel == pattern || strings.HasPrefix(rel, pattern+"/"))
	}

	return matchSlashPattern(pattern, filepath.ToSlash(name))
}

func matchSlashPattern(pattern, value string) bool {
	ok, err := pathpkg.Match(pattern, value)
	if err == nil {
		return ok
	}
	return pattern == value
}
