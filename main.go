package main

import (
	"bufio"
	"bytes"
	"container/heap"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type result struct {
	RelPath string
	Kind    uint8
	Score   int
}

type index struct {
	rootAbs string
	files   []string
}

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
}

const (
	kindDir  uint8 = 0
	kindFile uint8 = 1
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

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.root, "root", "/", "Root directory to search")
	flag.StringVar(&cfg.mode, "type", "all", "Search type: file|dir|all")
	flag.IntVar(&cfg.limit, "limit", 100, "Max number of results")
	flag.BoolVar(&cfg.hidden, "hidden", false, "Include hidden files (passed to rg)")
	flag.BoolVar(&cfg.noIgnore, "no-ignore", false, "Ignore .gitignore and other ignore rules")
	flag.StringVar(&cfg.color, "color", "auto", "Color mode: auto|always|never")
	flag.BoolVar(&cfg.interactive, "i", false, "Force interactive mode")
	flag.StringVar(&cfg.text, "text", "", "Search text inside files (content search)")
	flag.StringVar(&cfg.glob, "glob", "", "Limit file paths using glob pattern (e.g. '*.txt')")
	flag.BoolVar(&cfg.progress, "progress", true, "Show progress for large scans/matches")
	flag.BoolVar(&cfg.once, "once", false, "Run a single query and exit")
	flag.StringVar(&cfg.ui, "ui", "auto", "Interactive UI: auto|plain|fzf")
	flag.StringVar(&cfg.editor, "editor", "", "Editor command for opening selections (default: $VISUAL/$EDITOR)")
	flag.Usage = usage
	flag.Parse()

	cfg.mode = strings.ToLower(strings.TrimSpace(cfg.mode))
	cfg.color = strings.ToLower(strings.TrimSpace(cfg.color))
	cfg.text = strings.TrimSpace(cfg.text)
	cfg.glob = strings.TrimSpace(cfg.glob)
	cfg.ui = strings.ToLower(strings.TrimSpace(cfg.ui))
	cfg.editor = strings.TrimSpace(cfg.editor)

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
	return cfg
}

func usage() {
	fmt.Fprintf(os.Stderr, `terminal_search: fast file + directory finder

Usage:
  terminal_search [flags]
  terminal_search [flags] <query>

Notes:
  - Starts interactive mode by default.
  - If a query is provided, it runs that query first, then stays interactive.
  - Use -once to run a single query and exit.
  - Use -text for file content search (optionally with -glob '*.txt').

Flags:
`)
	flag.PrintDefaults()
}

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
	if initialQuery != "" {
		matches := rankIndex(idx, initialQuery, cfg.limit, cfg.mode, cfg.progress)
		renderMatches(os.Stdout, idx.rootAbs, matches, wantsColor(cfg.color))
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
			}
			continue
		}

		if line != "" {
			matches := rankIndex(idx, line, cfg.limit, cfg.mode, cfg.progress)
			renderMatches(os.Stdout, idx.rootAbs, matches, wantsColor(cfg.color))
			fmt.Println()
		}

		if errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}

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
		seen := map[string]struct{}{}
		for _, rel := range idx.files {
			dir := filepath.Dir(rel)
			for dir != "." && dir != string(filepath.Separator) && dir != "" {
				if _, ok := seen[dir]; ok {
					next := filepath.Dir(dir)
					if next == dir {
						break
					}
					dir = next
					continue
				}
				seen[dir] = struct{}{}
				abs := filepath.Join(idx.rootAbs, dir)
				fmt.Fprintf(w, "%d\tDIR\t%s\n", kindDir, abs)

				next := filepath.Dir(dir)
				if next == dir {
					break
				}
				dir = next
			}
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

func runSearch(w io.Writer, cfg config, query string) error {
	idx, err := prepareIndex(cfg)
	if err != nil {
		return err
	}
	matches := rankIndex(idx, query, cfg.limit, cfg.mode, cfg.progress)
	renderMatches(w, idx.rootAbs, matches, wantsColor(cfg.color))
	return nil
}

func runContentSearch(w io.Writer, cfg config, nameQuery string) error {
	rootAbs, err := filepath.Abs(cfg.root)
	if err != nil {
		return err
	}
	files, err := listFilesByContentRG(cfg)
	if err != nil {
		return err
	}

	var matches []result
	if nameQuery == "" {
		if cfg.limit > 0 && len(files) > cfg.limit {
			files = files[:cfg.limit]
		}
		matches = make([]result, 0, len(files))
		for _, f := range files {
			matches = append(matches, result{RelPath: f, Kind: kindFile, Score: 0})
		}
	} else {
		idx := index{rootAbs: rootAbs, files: files}
		matches = rankIndex(idx, nameQuery, cfg.limit, "file", cfg.progress)
	}

	renderMatches(w, rootAbs, matches, wantsColor(cfg.color))
	return nil
}

func prepareIndex(cfg config) (index, error) {
	rootAbs, err := filepath.Abs(cfg.root)
	if err != nil {
		return index{}, err
	}

	files, err := listFiles(cfg)
	if err != nil {
		return index{}, err
	}

	idx := index{
		rootAbs: rootAbs,
		files:   files,
	}
	return idx, nil
}

func renderMatches(w io.Writer, rootAbs string, matches []result, useColor bool) {
	if len(matches) == 0 {
		fmt.Fprintln(w, "no matches")
		return
	}

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

func listFiles(cfg config) ([]string, error) {
	if _, err := exec.LookPath("rg"); err == nil {
		return listFilesWithRG(cfg)
	}
	return listFilesWithWalk(cfg.root, cfg.progress)
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
	return runRGPathList(cfg, args, "indexing")
}

func listFilesByContentRG(cfg config) ([]string, error) {
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
	return runRGPathList(cfg, args, "content-scan")
}

func runRGPathList(cfg config, args []string, label string) ([]string, error) {
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
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		clean := filepath.Clean(line)
		if clean == "." {
			continue
		}
		results = append(results, clean)
		count++
		if count%512 == 0 {
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
		// Permission-denied paths can still produce valid partial output.
		if len(results) > 0 {
			return results, nil
		}
		msg := strings.TrimSpace(stderrBuf.String())
		if msg != "" {
			return nil, fmt.Errorf("rg failed: %s", msg)
		}
		return nil, fmt.Errorf("rg failed: %w", waitErr)
	}
	return results, nil
}

func listFilesWithWalk(root string, showProgress bool) ([]string, error) {
	results := make([]string, 0, 8192)
	meter := newProgressMeter("indexing", 0, showProgress)
	count := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Ignore inaccessible paths to keep whole-system scans moving.
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.Clean(rel)
		if rel != "." {
			results = append(results, rel)
			count++
			if count%512 == 0 {
				meter.TickUnknown(count)
			}
		}
		return nil
	})

	meter.TickUnknown(count)
	meter.FinishUnknown(count)
	return results, err
}

func rankIndex(idx index, query string, limit int, mode string, showProgress bool) []result {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if limit <= 0 {
		limit = 100
	}

	total := 0
	knownTotal := mode == "file"
	if knownTotal {
		total += len(idx.files)
	}
	meter := newProgressMeter("matching", total, showProgress)

	useWildcard := hasWildcards(q)
	pattern := strings.ToLower(q)
	literalLen := 0
	if useWildcard {
		literalLen = wildcardLiteralLen(pattern)
	}

	h := &resultMinHeap{}
	heap.Init(h)

	step := 0
	consume := func(path string, kind uint8) {
		var score int
		var ok bool
		if useWildcard {
			score, ok = wildcardScore(pattern, path, literalLen)
		} else {
			score, ok = fuzzyScore(q, path)
		}
		if ok {
			pushTopK(h, result{RelPath: path, Kind: kind, Score: score}, limit)
		}

		step++
		if step%512 == 0 {
			if knownTotal {
				meter.TickKnown(step)
			} else {
				meter.TickUnknown(step)
			}
		}
	}

	dirSeen := map[string]struct{}{}

	for _, p := range idx.files {
		if mode == "file" || mode == "all" {
			consume(p, kindFile)
		}
		if mode == "dir" || mode == "all" {
			dir := filepath.Dir(p)
			for dir != "." && dir != string(filepath.Separator) && dir != "" {
				if _, exists := dirSeen[dir]; !exists {
					dirSeen[dir] = struct{}{}
					consume(dir, kindDir)
				}
				next := filepath.Dir(dir)
				if next == dir {
					break
				}
				dir = next
			}
		}
	}

	if knownTotal {
		meter.TickKnown(step)
		meter.FinishKnown(step)
	} else {
		meter.TickUnknown(step)
		meter.FinishUnknown(step)
	}

	out := make([]result, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(result)
	}
	return out
}

func pushTopK(h *resultMinHeap, r result, limit int) {
	if h.Len() < limit {
		heap.Push(h, r)
		return
	}
	if isBetter(r, (*h)[0]) {
		(*h)[0] = r
		heap.Fix(h, 0)
	}
}

type resultMinHeap []result

func (h resultMinHeap) Len() int { return len(h) }

func (h resultMinHeap) Less(i, j int) bool {
	return isWorse(h[i], h[j])
}

func (h resultMinHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *resultMinHeap) Push(x any) {
	*h = append(*h, x.(result))
}

func (h *resultMinHeap) Pop() any {
	old := *h
	n := len(old)
	v := old[n-1]
	*h = old[:n-1]
	return v
}

func isBetter(a, b result) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if len(a.RelPath) != len(b.RelPath) {
		return len(a.RelPath) < len(b.RelPath)
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.RelPath < b.RelPath
}

func isWorse(a, b result) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	if len(a.RelPath) != len(b.RelPath) {
		return len(a.RelPath) > len(b.RelPath)
	}
	if a.Kind != b.Kind {
		return a.Kind > b.Kind
	}
	return a.RelPath > b.RelPath
}

func hasWildcards(q string) bool {
	return strings.ContainsAny(q, "*?")
}

func wildcardLiteralLen(pattern string) int {
	n := 0
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '*' && pattern[i] != '?' {
			n++
		}
	}
	return n
}

func wildcardScore(pattern, relPath string, literalLen int) (int, bool) {
	rel := strings.ToLower(relPath)
	base := strings.ToLower(filepath.Base(relPath))

	best := -1
	if wildcardMatch(pattern, rel) {
		best = 2400 - (len(rel) - literalLen)
	}
	if wildcardMatch(pattern, base) {
		baseScore := 3200 - (len(base) - literalLen)
		if baseScore > best {
			best = baseScore
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

func wildcardMatch(pattern, s string) bool {
	p := 0
	i := 0
	star := -1
	match := 0

	for i < len(s) {
		if p < len(pattern) && (pattern[p] == '?' || pattern[p] == s[i]) {
			p++
			i++
			continue
		}
		if p < len(pattern) && pattern[p] == '*' {
			star = p
			match = i
			p++
			continue
		}
		if star != -1 {
			p = star + 1
			match++
			i = match
			continue
		}
		return false
	}

	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

func fuzzyScore(query, path string) (int, bool) {
	q := strings.ToLower(query)
	p := strings.ToLower(path)

	score, ok := fuzzyScoreOnly(q, p, len(path), len(query))
	if !ok {
		return 0, false
	}

	base := filepath.Base(path)
	if base != path {
		baseScore, baseOK := fuzzyScoreOnly(q, strings.ToLower(base), len(base), len(query))
		if baseOK && baseScore+120 > score {
			score = baseScore + 120
		}
	}
	return score, true
}

func fuzzyScoreOnly(q, p string, pathLen, queryLen int) (int, bool) {
	if idx := strings.Index(p, q); idx >= 0 {
		score := 3000 - idx*4 - (pathLen - queryLen)
		return score, true
	}

	qi := 0
	last := -1
	score := 0
	streak := 0

	for i := 0; i < len(p) && qi < len(q); i++ {
		if p[i] != q[qi] {
			continue
		}
		score += 24
		if last >= 0 {
			gap := i - last - 1
			score -= gap
			if gap == 0 {
				streak++
				score += 16 + streak*2
			} else {
				streak = 0
			}
		}
		last = i
		qi++
	}

	if qi != len(q) {
		return 0, false
	}

	score -= (pathLen - queryLen)
	score += 800
	return score, true
}

type progressMeter struct {
	label   string
	total   int
	enabled bool
	last    time.Time
	spin    int
}

func newProgressMeter(label string, total int, enabled bool) *progressMeter {
	isTTY := enabled && fileIsTTY(os.Stderr)
	if !isTTY {
		return &progressMeter{enabled: false}
	}

	// Avoid noisy output for tiny operations.
	if total > 0 && total < 200000 {
		return &progressMeter{enabled: false}
	}

	return &progressMeter{
		label:   label,
		total:   total,
		enabled: true,
		last:    time.Now().Add(-time.Second),
	}
}

func (m *progressMeter) TickKnown(done int) {
	if !m.enabled {
		return
	}
	now := time.Now()
	if now.Sub(m.last) < 120*time.Millisecond && done < m.total {
		return
	}
	if done < 0 {
		done = 0
	}
	if done > m.total {
		done = m.total
	}

	const width = 24
	filled := 0
	percent := 100.0
	if m.total > 0 {
		filled = int(float64(done) / float64(m.total) * float64(width))
		if filled < 0 {
			filled = 0
		}
		if filled > width {
			filled = width
		}
		percent = float64(done) / float64(m.total) * 100
	}
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
	fmt.Fprintf(os.Stderr, "\r%s [%s] %5.1f%% (%d/%d)", m.label, bar, percent, done, m.total)
	m.last = now
}

func (m *progressMeter) TickUnknown(done int) {
	if !m.enabled {
		return
	}
	now := time.Now()
	if now.Sub(m.last) < 120*time.Millisecond {
		return
	}
	spinner := []string{"|", "/", "-", "\\"}
	fmt.Fprintf(os.Stderr, "\r%s %s %d", m.label, spinner[m.spin%len(spinner)], done)
	m.spin++
	m.last = now
}

func (m *progressMeter) FinishKnown(done int) {
	if !m.enabled {
		return
	}
	m.TickKnown(done)
	fmt.Fprintln(os.Stderr)
}

func (m *progressMeter) FinishUnknown(done int) {
	if !m.enabled {
		return
	}
	fmt.Fprintf(os.Stderr, "\r%s done: %d\n", m.label, done)
}

func kindString(kind uint8) string {
	if kind == kindDir {
		return "dir"
	}
	return "file"
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
