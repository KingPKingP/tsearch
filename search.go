package main

import (
	"io"
	"path/filepath"
)

func runSearch(w io.Writer, cfg config, query string) error {
	idx, err := prepareIndex(cfg)
	if err != nil {
		return err
	}
	matches := rankIndex(idx, query, cfg.limit, cfg.mode, cfg.progress)
	renderMatches(w, idx.rootAbs, matches, cfg)
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

	renderMatches(w, rootAbs, matches, cfg)
	return nil
}

func prepareIndex(cfg config) (index, error) {
	rootAbs, err := filepath.Abs(cfg.root)
	if err != nil {
		return index{}, err
	}

	files, dirs, err := listPaths(cfg)
	if err != nil {
		return index{}, err
	}

	return index{
		rootAbs: rootAbs,
		files:   files,
		dirs:    dirs,
	}, nil
}
