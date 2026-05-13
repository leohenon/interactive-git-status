package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type area int

const (
	unstaged area = iota
	staged
)

type item struct {
	area   area
	status string
	path   string
}

type app struct {
	items     []item
	cursor    int
	err       string
	lastLines int
}

func main() {
	if !isGitRepo() {
		fmt.Fprintln(os.Stderr, "not a git repository")
		os.Exit(1)
	}

	oldState, _ := exec.Command("stty", "-g").Output()
	_ = exec.Command("stty", "raw", "-echo").Run()
	defer func() {
		_ = exec.Command("stty", string(bytes.TrimSpace(oldState))).Run()
		fmt.Print("\033[?25h\033[0m\n")
	}()

	a := &app{}
	a.refresh()
	a.draw()

	r := bufio.NewReader(os.Stdin)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return
		}

		switch b {
		case 'q', 3:
			return
		case 'j':
			a.down()
		case 'k':
			a.up()
		case 'r':
			a.refresh()
		case ' ', '\t':
			a.toggle()
		case 's':
			a.stage()
		case 'u':
			a.unstage()
		case '\033':
			// Arrow keys: ESC [ A/B
			if next, _ := r.Peek(2); len(next) == 2 && next[0] == '[' {
				_, _ = r.Discard(2)
				switch next[1] {
				case 'A':
					a.up()
				case 'B':
					a.down()
				}
			}
		}

		a.draw()
	}
}

func (a *app) draw() {
	if a.lastLines > 0 {
		fmt.Printf("\033[%dA\033[J", a.lastLines)
	}

	out := a.render()
	fmt.Print(out)
	a.lastLines = strings.Count(out, "\n")
}

func (a *app) render() string {
	var b strings.Builder

	branch := gitOutput("branch", "--show-current")
	if branch == "" {
		branch = "HEAD"
	}
	fmt.Fprintf(&b, "On branch %s\n\n", branch)

	unstagedItems := a.itemsIn(unstaged)
	stagedItems := a.itemsIn(staged)

	fmt.Fprintf(&b, "Unstaged changes (%d)\n", len(unstagedItems))
	a.renderItems(&b, unstaged)
	if len(unstagedItems) == 0 {
		b.WriteString("  none\n")
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "Staged changes (%d)\n", len(stagedItems))
	a.renderItems(&b, staged)
	if len(stagedItems) == 0 {
		b.WriteString("  none\n")
	}

	if a.err != "" {
		fmt.Fprintf(&b, "\n%s\n", a.err)
	}

	return b.String()
}

func (a *app) renderItems(b *strings.Builder, which area) {
	for i, it := range a.items {
		if it.area != which {
			continue
		}

		marker := " "
		if i == a.cursor {
			marker = ">"
		}
		fmt.Fprintf(b, "%s %-10s %s\n", marker, statusName(it.status), it.path)
	}
}

func (a *app) refresh() {
	items, err := statusItems()
	if err != nil {
		a.err = err.Error()
		return
	}

	a.items = items
	a.err = ""
	if a.cursor >= len(a.items) {
		a.cursor = len(a.items) - 1
	}
	if a.cursor < 0 {
		a.cursor = 0
	}
}

func (a *app) up() {
	if a.cursor > 0 {
		a.cursor--
	}
}

func (a *app) down() {
	if a.cursor < len(a.items)-1 {
		a.cursor++
	}
}

func (a *app) toggle() {
	if len(a.items) == 0 {
		return
	}
	if a.items[a.cursor].area == staged {
		a.unstage()
	} else {
		a.stage()
	}
}

func (a *app) stage() {
	if len(a.items) == 0 || a.items[a.cursor].area != unstaged {
		return
	}
	a.run("add", "--", a.items[a.cursor].path)
}

func (a *app) unstage() {
	if len(a.items) == 0 || a.items[a.cursor].area != staged {
		return
	}
	if hasHead() {
		a.run("restore", "--staged", "--", a.items[a.cursor].path)
	} else {
		a.run("rm", "--cached", "-r", "--", a.items[a.cursor].path)
	}
}

func (a *app) run(args ...string) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		a.err = strings.TrimSpace(string(out))
		if a.err == "" {
			a.err = err.Error()
		}
		return
	}
	a.refresh()
}

func (a *app) itemsIn(which area) []item {
	var out []item
	for _, it := range a.items {
		if it.area == which {
			out = append(out, it)
		}
	}
	return out
}

func statusItems() ([]item, error) {
	cmd := exec.Command("git", "status", "--porcelain=v1")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var items []item
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}

		x := line[0]
		y := line[1]
		path := line[3:]
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}

		if x == '?' && y == '?' {
			items = append(items, item{area: unstaged, status: "??", path: path})
			continue
		}
		if y != ' ' {
			items = append(items, item{area: unstaged, status: string(y), path: path})
		}
		if x != ' ' {
			items = append(items, item{area: staged, status: string(x), path: path})
		}
	}

	return items, nil
}

func statusName(s string) string {
	switch s {
	case "M":
		return "modified"
	case "A":
		return "added"
	case "D":
		return "deleted"
	case "R":
		return "renamed"
	case "C":
		return "copied"
	case "??":
		return "untracked"
	default:
		return s
	}
}

func gitOutput(args ...string) string {
	out, _ := exec.Command("git", args...).Output()
	return strings.TrimSpace(string(out))
}

func isGitRepo() bool {
	return exec.Command("git", "rev-parse", "--is-inside-work-tree").Run() == nil
}

func hasHead() bool {
	return exec.Command("git", "rev-parse", "--verify", "HEAD").Run() == nil
}
