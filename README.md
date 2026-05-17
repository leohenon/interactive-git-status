<h1 align="center">interactive-git-status</h1>

<p align="center">
  <a href="https://github.com/leohenon/interactive-git-status/releases"><img src="https://img.shields.io/github/v/release/leohenon/interactive-git-status?style=flat-square&logo=github&logoColor=white&color=85c1e9" alt="Release"></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.23%2B-85c1e9?style=flat-square&logo=go&logoColor=white" alt="Go 1.23+"></a>
  <a href="https://github.com/leohenon/interactive-git-status/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/leohenon/interactive-git-status/ci.yml?style=flat-square&logo=github&logoColor=white&label=CI&color=3fb950" alt="CI"></a>
</p>

<p align="center">
  A minimal interactive <code>git status</code> for faster staging.
</p>

## Install

Homebrew:

```sh
brew install leohenon/tap/igs
```

Go:

```sh
go install github.com/leohenon/interactive-git-status/cmd/igs@latest
```

## Usage

```sh
igs
```

![igs default view](assets/igs.gif)

```sh
igs --short
```

![igs short view](assets/igs-short.gif)

Press `c` to commit staged changes.

![igs commit view](assets/igs-commit.gif)

Press `d` to open the selected file's diff using your configured Git pager.

![igs diff view](assets/igs-diff.gif)

Customize via Git config:

```sh
git config --global igs.diffPager 'delta --paging=never --width {width} | less -R'
```

## Flags

| Flag            | Description                                               |
| --------------- | --------------------------------------------------------- |
| `-s`, `--short` | Use compact interactive status view                       |
| `--ignored`     | Show ignored files                                        |
| `--show-stash`  | Show stash count. Also shown when `status.showStash=true` |
| `--watch`       | Refresh status automatically                              |
| `--version`     | Show version                                              |
| `-h`, `--help`  | Show usage                                                |

## Keybindings

| Key                  | Action                               |
| -------------------- | ------------------------------------ |
| `enter`              | Toggle selected file staged/unstaged |
| `s`                  | Stage selected file                  |
| `u`                  | Unstage selected file                |
| `a`                  | Toggle all for current side          |
| `S`                  | Stage all                            |
| `U`                  | Unstage all                          |
| `c`                  | Commit staged changes                |
| `d`                  | Show diff for selected file          |
| `r`                  | Refresh                              |
| `j`, `↓`             | Move down                            |
| `k`, `↑`             | Move up                              |
| `ctrl-d`             | Move down half a screen              |
| `ctrl-u`             | Move up half a screen                |
| `g`                  | Jump to top                          |
| `G`                  | Jump to bottom                       |
| `tab`                | Jump to next section                 |
| `q`, `esc`, `ctrl-c` | Exit                                 |

## Why

Keep the familiar `git status` view, but stage, unstage, and commit interactively.

## What it does

- Supports staged, unstaged, untracked, ignored, and unmerged files
- Shows branch, upstream, stash, sparse checkout, detached HEAD, and in-progress operation state
- Handles renamed files, copied files, submodules, and paths with spaces

> Supports macOS and Linux.

## Example aliases

```sh
alias gs='igs'
alias gss='igs --short'
```

## License

MIT
