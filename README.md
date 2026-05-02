
Simple, slick, and fast terminal CLI to search files and directories.

## Why it is fast

- Uses streamed `rg` output (no giant single-buffer capture).
- Falls back to Go `filepath.WalkDir` if `rg` is not installed.
- Uses a top-K heap for ranking (keeps only best `-limit` matches in memory).
- Keeps files and directories in separate indexes so empty directories can be found.

## Build

```bash
go build -o tsearch .
```

## Usage

```bash
./tsearch [flags]          # interactive by default
./tsearch [flags] <query>  # runs first query, then stays interactive
```

Interactive UI:

- `-ui auto` (default): uses `fzf` when available in a real TTY, otherwise plain prompt mode.
- `-ui fzf`: force `fzf` TUI.
- `-ui plain`: force line-prompt mode.

`fzf` key bindings:

- `Enter` or `Ctrl-O`: open selected path in your editor
- `Ctrl-Y`: copy selected path to clipboard
- `Ctrl-R`: reindex and refresh list

Text content search:

```bash
./tsearch -text "needle" -glob '*.txt'
./tsearch -root /path -text "password" -glob '*.md'
```

Output format:

- `/absolute/path <dir>`
- `/absolute/path <file>`
- `-format plain` prints only paths, one per line
- `-format jsonl` prints one JSON object per result
- `-0` prints NUL-separated paths for shell pipelines

Wildcard search:

- `*` matches any sequence of characters
- `?` matches a single character
- Example: `'*.txt'` (quote wildcard in shell)

Ranking:

- Exact basename and path-segment matches rank higher than embedded matches.
- Matching is case-insensitive UTF-8 byte matching; it does not normalize Unicode.

Color coding:

- Directories: cyan
- Go files: bright green
- Code files: blue
- Config files: magenta
- Docs: gray
- Archives: red
- Images: bright magenta
- Media/executable/hidden files have their own styles

### Flags

- `-root` root directory to search (default current directory)
- `-type` `file|dir|all` (default `all`)
- `-limit` max results (default `100`)
- `-hidden` include hidden files
- `-no-ignore` ignore `.gitignore` and other ignore rules
- `-color` `auto|always|never` (default `auto`)
- `-i` force interactive mode (`reload` command refreshes the index)
- `-text` search text inside files (content search mode)
- `-glob` filter indexed/searched files by glob (e.g. `*.txt`)
- `-progress` show progress during large scans/matches (default `true`)
- `-once` run a single query and exit
- `-ui` interactive UI mode: `auto|plain|fzf`
- `-editor` editor command for open action (default `$VISUAL` / `$EDITOR`)
- `-format` output format: `human|plain|jsonl` (default `human`)
- `-0` use NUL-separated plain paths

## Examples

```bash
# Search both files and directories
./tsearch auth

# One-shot mode (exit directly after output)
./tsearch -once auth

# Interactive search in current directory
./tsearch

# Files only
./tsearch -type file handler

# Directories only
./tsearch -type dir srcapi

# Search another root
./tsearch -root ~/projects client

# Wildcard search
./tsearch '*.txt'

# Search text inside text files
./tsearch -root ~/projects -text "TODO" -glob '*.txt'

# Interactive mode
./tsearch -i -root ~/projects

# Force fzf TUI + custom editor
./tsearch -ui fzf -editor "nvim" -root ~/projects
```
