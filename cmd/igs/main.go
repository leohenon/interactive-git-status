package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type area int

const (
	unstaged area = iota
	untracked
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
	itemRows  []int
}

func main() {
	if !isGitRepo() {
		fmt.Fprintln(os.Stderr, "not a git repository")
		os.Exit(1)
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer tty.Close()

	oldState, _ := stty(tty, "-g")
	_ = runStty(tty, "cbreak", "-echo")
	fmt.Print("\033[?25l")
	defer func() {
		_ = runStty(tty, string(bytes.TrimSpace(oldState)))
		fmt.Print("\033[?25h\033[0m\n")
	}()

	a := &app{}
	a.refresh()
	a.draw()

	r := bufio.NewReader(tty)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return
		}

		oldCursor := a.cursor
		redraw := false

		switch b {
		case 'q', 3:
			return
		case '\r', '\n':
			a.toggle()
			redraw = true
		case 'j':
			a.down()
		case 'k':
			a.up()
		case 'r':
			a.refresh()
			redraw = true
		case '\t':
			a.nextSection()
		case 's':
			a.stage()
			redraw = true
		case 'u':
			a.unstage()
			redraw = true
		case '\033':
			// Arrow keys: ESC [ A/B. Plain ESC exits.
			switch readEscapeSequence(tty, r) {
			case "[A":
				a.up()
			case "[B":
				a.down()
			default:
				return
			}
		}

		if redraw {
			a.draw()
		} else if oldCursor != a.cursor {
			a.moveCursor(oldCursor)
		}
	}
}

func (a *app) draw() {
	if a.lastLines > 0 {
		fmt.Printf("\r\033[%dA\033[J", a.lastLines)
	}

	out := a.render()
	fmt.Print(out)
	a.lastLines = strings.Count(out, "\n")
}

func (a *app) moveCursor(oldCursor int) {
	a.rewriteItem(oldCursor)
	a.rewriteItem(a.cursor)
}

func (a *app) rewriteItem(index int) {
	if index < 0 || index >= len(a.items) || index >= len(a.itemRows) {
		return
	}

	row := a.itemRows[index]
	if row == 0 || a.lastLines == 0 {
		return
	}

	up := a.lastLines - row + 1
	fmt.Printf("\r\033[%dA\033[K%s\033[%dB\r", up, a.itemLine(index), up)
}

func (a *app) itemLine(index int) string {
	marker := " "
	if index == a.cursor {
		marker = ">"
	}
	it := a.items[index]
	return fmt.Sprintf("%s %-10s %s", marker, statusName(it.status), it.path)
}

func (a *app) render() string {
	var b strings.Builder
	line := 0
	a.itemRows = make([]int, len(a.items))

	write := func(s string) {
		b.WriteString(s)
		line++
	}

	branch := gitOutput("branch", "--show-current")
	if branch == "" {
		branch = "HEAD"
	}
	write(fmt.Sprintf("On branch %s\n", branch))
	write("\n")

	unstagedItems := a.itemsIn(unstaged)
	untrackedItems := a.itemsIn(untracked)
	stagedItems := a.itemsIn(staged)

	write(fmt.Sprintf("Unstaged changes (%d)\n", len(unstagedItems)))
	a.renderItems(&b, &line, unstaged)
	if len(unstagedItems) == 0 {
		write("  none\n")
	}

	write("\n")
	write(fmt.Sprintf("Untracked files (%d)\n", len(untrackedItems)))
	a.renderItems(&b, &line, untracked)
	if len(untrackedItems) == 0 {
		write("  none\n")
	}

	write("\n")
	write(fmt.Sprintf("Staged changes (%d)\n", len(stagedItems)))
	a.renderItems(&b, &line, staged)
	if len(stagedItems) == 0 {
		write("  none\n")
	}

	if a.err != "" {
		write("\n")
		write(fmt.Sprintf("%s\n", a.err))
	}

	return b.String()
}

func (a *app) renderItems(b *strings.Builder, line *int, which area) {
	for i := range a.items {
		if a.items[i].area != which {
			continue
		}

		a.itemRows[i] = *line + 1
		fmt.Fprintf(b, "%s\n", a.itemLine(i))
		*line++
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

func (a *app) nextSection() {
	if len(a.items) == 0 {
		return
	}

	sections := []area{unstaged, untracked, staged}
	current := a.items[a.cursor].area
	start := 0
	for i, section := range sections {
		if section == current {
			start = i + 1
			break
		}
	}

	for offset := 0; offset < len(sections); offset++ {
		section := sections[(start+offset)%len(sections)]
		if index := a.firstIn(section); index >= 0 {
			a.cursor = index
			return
		}
	}
}

func (a *app) firstIn(which area) int {
	for i, it := range a.items {
		if it.area == which {
			return i
		}
	}
	return -1
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
	if len(a.items) == 0 || a.items[a.cursor].area == staged {
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

func readEscapeSequence(tty *os.File, r *bufio.Reader) string {
	if r.Buffered() >= 2 {
		next, _ := r.Peek(2)
		_, _ = r.Discard(2)
		return string(next)
	}

	fd := int(tty.Fd())
	_ = syscall.SetNonblock(fd, true)
	defer syscall.SetNonblock(fd, false)

	time.Sleep(10 * time.Millisecond)
	buf := make([]byte, 2)
	n, _ := tty.Read(buf)
	return string(buf[:n])
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
			items = append(items, item{area: untracked, status: "??", path: path})
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

func stty(tty *os.File, args ...string) ([]byte, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = tty
	return cmd.Output()
}

func runStty(tty *os.File, args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	return cmd.Run()
}

func isGitRepo() bool {
	return exec.Command("git", "rev-parse", "--is-inside-work-tree").Run() == nil
}

func hasHead() bool {
	return exec.Command("git", "rev-parse", "--verify", "HEAD").Run() == nil
}
