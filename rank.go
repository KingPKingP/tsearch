package main

import (
	"container/heap"
	"path/filepath"
	"strings"
)

type result struct {
	RelPath string
	Kind    uint8
	Score   int
}

const (
	kindDir  uint8 = 0
	kindFile uint8 = 1
)

func rankIndex(idx index, query string, limit int, mode string, showProgress bool) []result {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	if limit <= 0 {
		limit = 100
	}

	total := 0
	switch mode {
	case "file":
		total = len(idx.files)
	case "dir":
		total = len(idx.dirs)
	default:
		total = len(idx.files) + len(idx.dirs)
	}
	meter := newProgressMeter("matching", total, showProgress && total > 0)

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
			meter.TickKnown(step)
		}
	}

	if mode == "file" || mode == "all" {
		for _, p := range idx.files {
			consume(p, kindFile)
		}
	}
	if mode == "dir" || mode == "all" {
		for _, p := range idx.dirs {
			consume(p, kindDir)
		}
	}

	meter.TickKnown(step)
	meter.FinishKnown(step)

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
		if baseOK && baseScore+160 > score {
			score = baseScore + 160
		}
	}
	return score, true
}

func fuzzyScoreOnly(q, p string, pathLen, queryLen int) (int, bool) {
	if idx := strings.Index(p, q); idx >= 0 {
		score := 3000 - idx*4 - (pathLen - queryLen)
		if idx == 0 {
			score += 260
		} else if isBoundaryAt(p, idx) {
			score += 180
		}
		if idx+len(q) == len(p) {
			score += 60
		}
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
		if isBoundaryAt(p, i) {
			score += 18
		}
		if last >= 0 {
			gap := i - last - 1
			score -= gap
			if gap == 0 {
				streak++
				score += 18 + streak*3
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

func isBoundaryAt(s string, i int) bool {
	if i <= 0 {
		return true
	}
	prev := s[i-1]
	cur := s[i]
	if isPathWordSeparator(prev) {
		return true
	}
	return isDigit(prev) && isLetter(cur)
}

func isPathWordSeparator(c byte) bool {
	switch c {
	case '/', '\\', '-', '_', '.', ' ':
		return true
	default:
		return false
	}
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z'
}

func kindString(kind uint8) string {
	if kind == kindDir {
		return "dir"
	}
	return "file"
}
