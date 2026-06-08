<h1 align="center">interactive-git-status</h1>

<p align="center">
  <a href="https://github.com/leohenon/interactive-git-status/releases"><img src="https://img.shields.io/github/v/release/leohenon/interactive-git-status?style=flat-square&logo=github&logoColor=white&color=85c1e9" alt="Release"></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.23%2B-85c1e9?style=flat-square&logo=go&logoColor=white" alt="Go 1.23+"></a>
</p>

<p align="center">
  Minimal interactive <code>git status</code> for staging, unstaging, committing, diffing, and opening files.
</p>

## Install

```sh
# Go
go install github.com/leohenon/interactive-git-status/cmd/igs@latest

# Brew
brew install leohenon/tap/igs
```

## Usage

```sh
igs
```

![igs](assets/igs.gif)

## Keys

- `enter` toggle stage
- `s` / `u` stage / unstage
- `p` / `P` stage hunks in selected file / all files
- `S` / `U` stage / unstage all
- `a` toggle current side
- `c` commit
- `d` / `D` diff selected / staged
- `x` discard selected file
- `o` open in `$EDITOR`
- `r` refresh
- `j`/`k`, `↓`/`↑` move
- `ctrl-d` / `ctrl-u` half page
- `g` / `G` top / bottom
- `tab` next section
- `q`, `esc`, `ctrl-c` exit

## Flags

- `-s`, `--short` compact view
- `--ignored` show ignored files
- `--show-stash` show stash
- `--watch` auto-refresh
- `--version` print version
- `-h`, `--help` print help

## License

MIT
