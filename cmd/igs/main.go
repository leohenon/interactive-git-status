package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
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
	branch    string
	upstream  string
}

type colors struct {
	added     string
	changed   string
	untracked string
}

var gitColors = colors{
	added:     "\033[32m",
	changed:   "\033[31m",
	untracked: "\033[31m",
}

func main() {
	a := &app{}
	a.refresh()
	if a.err != "" {
		fmt.Fprintln(os.Stderr, a.err)
		os.Exit(1)
	}
	if len(a.items) == 0 {
		fmt.Print(a.render())
		return
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer tty.Close()

	oldState, err := enableInputMode(tty)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print("\033[?25l")
	defer func() {
		if len(a.items) > 0 {
			a.clearCursorMarker()
		}
		_ = restoreInputMode(tty, oldState)
		fmt.Print("\033[?25h\033[0m")
	}()

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
			drainInput(tty, r)
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
		case 'a':
			a.toggleAll()
			drainInput(tty, r)
			redraw = true
		case 's':
			a.stage()
			drainInput(tty, r)
			redraw = true
		case 'S':
			a.stageAll()
			drainInput(tty, r)
			redraw = true
		case 'u':
			a.unstage()
			drainInput(tty, r)
			redraw = true
		case 'U':
			a.unstageAll()
			drainInput(tty, r)
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
	out := a.render()

	if a.lastLines > 0 {
		fmt.Printf("\r\033[%dA", a.lastLines)
	}

	fmt.Print(out)
	fmt.Print("\033[J")
	a.lastLines = strings.Count(out, "\n")
}

func (a *app) moveCursor(oldCursor int) {
	a.rewriteItem(oldCursor)
	a.rewriteItem(a.cursor)
}

func (a *app) clearCursorMarker() {
	a.rewriteItemAs(a.cursor, false)
}

func (a *app) rewriteItem(index int) {
	a.rewriteItemAs(index, index == a.cursor)
}

func (a *app) rewriteItemAs(index int, selected bool) {
	if index < 0 || index >= len(a.items) || index >= len(a.itemRows) {
		return
	}

	row := a.itemRows[index]
	if row == 0 || a.lastLines == 0 {
		return
	}

	up := a.lastLines - row + 1
	fmt.Printf("\r\033[%dA\033[K%s\033[%dB\r", up, a.itemLine(index, selected), up)
}

func (a *app) itemLine(index int, selected bool) string {
	marker := " "
	if selected {
		marker = "›"
	}

	it := a.items[index]
	status := statusName(it.status)
	if status == "" {
		return marker + "   " + colorPath(it)
	}

	return fmt.Sprintf("%s   %s   %s", marker, colorStatus(it), colorPath(it))
}

func (a *app) render() string {
	var b strings.Builder
	line := 0
	a.itemRows = make([]int, len(a.items))

	write := func(s string) {
		b.WriteString(strings.TrimSuffix(s, "\n"))
		b.WriteString("\033[K\r\n")
		line++
	}

	branch := a.branch
	if branch == "" {
		branch = "HEAD"
	}
	write(fmt.Sprintf("On branch %s\n", branch))
	if a.upstream != "" {
		write(fmt.Sprintf("Your branch is up to date with '%s'.\n", a.upstream))
	}

	unstagedItems := a.itemsIn(unstaged)
	untrackedItems := a.itemsIn(untracked)
	stagedItems := a.itemsIn(staged)

	if len(a.items) == 0 {
		if a.upstream != "" {
			write("\n")
		}
		write("nothing to commit, working tree clean\n")
		return b.String()
	}

	write("\n")
	if len(untrackedItems) > 0 {
		write(fmt.Sprintf("Untracked files (%d)\n", len(untrackedItems)))
		a.renderItems(&b, &line, untracked)
		write("\n")
	}

	write(fmt.Sprintf("Unstaged changes (%d)\n", len(unstagedItems)))
	a.renderItems(&b, &line, unstaged)
	if len(unstagedItems) == 0 {
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
		fmt.Fprintf(b, "%s\033[K\r\n", a.itemLine(i, i == a.cursor))
		*line++
	}
}

func (a *app) refresh() {
	branch, upstream, items, err := statusState()
	if err != nil {
		a.err = err.Error()
		return
	}

	a.branch = branch
	a.upstream = upstream
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

	sections := []area{untracked, unstaged, staged}
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
	a.runKeepingSection(a.items[a.cursor].area, "add", "--", a.items[a.cursor].path)
}

func (a *app) unstage() {
	if len(a.items) == 0 || a.items[a.cursor].area != staged {
		return
	}
	if hasHead() {
		a.runKeepingSection(staged, "restore", "--staged", "--", a.items[a.cursor].path)
	} else {
		a.runKeepingSection(staged, "rm", "--cached", "-r", "--", a.items[a.cursor].path)
	}
}

func (a *app) toggleAll() {
	if a.currentSection() == staged {
		a.unstageAll()
	} else {
		a.stageAll()
	}
}

func (a *app) stageAll() {
	a.runKeepingSection(a.currentSection(), "add", "-A")
}

func (a *app) unstageAll() {
	if hasHead() {
		a.runKeepingSection(a.currentSection(), "restore", "--staged", ":/")
	} else {
		a.runKeepingSection(a.currentSection(), "rm", "--cached", "-r", ":/")
	}
}

func (a *app) currentSection() area {
	if len(a.items) == 0 {
		return untracked
	}
	return a.items[a.cursor].area
}

func (a *app) runKeepingSection(section area, args ...string) {
	oldPath := ""
	oldIndex := a.cursor
	oldSectionIndex := a.indexInSection(oldIndex, section)
	if oldIndex >= 0 && oldIndex < len(a.items) {
		oldPath = a.items[oldIndex].path
	}

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
	a.cursor = a.cursorAfterAction(section, oldSectionIndex, oldPath)
}

func (a *app) cursorAfterAction(section area, oldSectionIndex int, oldPath string) int {
	items := a.itemsIn(section)
	if len(items) > 0 {
		if oldSectionIndex >= len(items) {
			oldSectionIndex = len(items) - 1
		}
		if oldSectionIndex < 0 {
			oldSectionIndex = 0
		}
		return a.nthInSection(section, oldSectionIndex)
	}

	for _, fallback := range fallbackSections(section) {
		if index := a.firstIn(fallback); index >= 0 {
			return index
		}
	}

	if oldPath != "" {
		for i, it := range a.items {
			if it.path == oldPath {
				return i
			}
		}
	}

	if a.cursor >= len(a.items) {
		return len(a.items) - 1
	}
	if a.cursor < 0 {
		return 0
	}
	return a.cursor
}

func fallbackSections(section area) []area {
	switch section {
	case untracked:
		return []area{unstaged, staged}
	case unstaged:
		return []area{untracked, staged}
	case staged:
		return []area{unstaged, untracked}
	default:
		return []area{untracked, unstaged, staged}
	}
}

func (a *app) indexInSection(index int, section area) int {
	sectionIndex := 0
	for i, it := range a.items {
		if it.area != section {
			continue
		}
		if i == index {
			return sectionIndex
		}
		sectionIndex++
	}
	return 0
}

func (a *app) nthInSection(section area, n int) int {
	sectionIndex := 0
	for i, it := range a.items {
		if it.area != section {
			continue
		}
		if sectionIndex == n {
			return i
		}
		sectionIndex++
	}
	return 0
}

func drainInput(tty *os.File, r *bufio.Reader) {
	for r.Buffered() > 0 {
		_, _ = r.Discard(r.Buffered())
	}

	fd := int(tty.Fd())
	_ = syscall.SetNonblock(fd, true)
	defer syscall.SetNonblock(fd, false)

	buf := make([]byte, 1024)
	for {
		n, _ := tty.Read(buf)
		if n == 0 {
			return
		}
	}
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

func statusState() (string, string, []item, error) {
	cmd := exec.Command("git", "status", "--porcelain=v1", "--branch", "--untracked-files=normal")
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if strings.Contains(message, "not a git repository") {
			message = "not a git repository"
		}
		if message == "" {
			message = err.Error()
		}
		return "", "", nil, fmt.Errorf("%s", message)
	}

	var branch string
	var upstream string
	var items []item
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			branch, upstream = parseBranchLine(strings.TrimPrefix(line, "## "))
			continue
		}
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

	sort.SliceStable(items, func(i, j int) bool {
		return areaOrder(items[i].area) < areaOrder(items[j].area)
	})

	return branch, upstream, items, nil
}

func parseBranchLine(line string) (string, string) {
	if strings.HasPrefix(line, "No commits yet on ") {
		return strings.TrimPrefix(line, "No commits yet on "), ""
	}

	branch := line
	upstream := ""
	if before, after, ok := strings.Cut(line, "..."); ok {
		branch = before
		upstream = after
		if i := strings.Index(upstream, " ["); i >= 0 {
			upstream = ""
		}
	}
	return branch, upstream
}

func areaOrder(which area) int {
	switch which {
	case untracked:
		return 0
	case unstaged:
		return 1
	case staged:
		return 2
	default:
		return 3
	}
}

func colorStatus(it item) string {
	return colorize(it, statusName(it.status))
}

func colorPath(it item) string {
	return colorize(it, it.path)
}

func colorize(it item, text string) string {
	color := ""
	switch it.area {
	case staged:
		color = gitColors.added
	case unstaged:
		color = gitColors.changed
	case untracked:
		color = gitColors.untracked
	}

	if color == "" {
		return text
	}
	return color + text + "\033[m"
}

func statusName(s string) string {
	switch s {
	case "M":
		return "modified:"
	case "A":
		return "new file:"
	case "D":
		return "deleted:"
	case "R":
		return "renamed:"
	case "C":
		return "copied:"
	case "??":
		return ""
	default:
		return s
	}
}

func enableInputMode(tty *os.File) (*term.State, error) {
	return term.MakeRaw(int(tty.Fd()))
}

func restoreInputMode(tty *os.File, state *term.State) error {
	if state == nil {
		return nil
	}
	return term.Restore(int(tty.Fd()), state)
}

func isGitRepo() bool {
	return exec.Command("git", "rev-parse", "--is-inside-work-tree").Run() == nil
}

func hasHead() bool {
	return exec.Command("git", "rev-parse", "--verify", "HEAD").Run() == nil
}
