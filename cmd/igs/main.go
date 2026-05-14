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
	unmerged area = iota
	untracked
	unstaged
	staged
	ignored
)

type options struct {
	ignored bool
}

type item struct {
	area   area
	status string
	path   string
	detail string
}

type app struct {
	items     []item
	cursor    int
	err       string
	lastLines int
	itemRows  []int
	branch    string
	oid       string
	initial   bool
	detached  bool
	upstream  string
	ahead     int
	behind    int
	stash     int
	operation operation
	options   options
}

type operation string

const (
	operationNone       operation = ""
	operationMerge      operation = "merge"
	operationRebase     operation = "rebase"
	operationCherryPick operation = "cherry-pick"
	operationRevert     operation = "revert"
)

type colors struct {
	added     string
	changed   string
	untracked string
	unmerged  string
	ignored   string
}

var gitColors = colors{
	added:     "\033[32m",
	changed:   "\033[31m",
	untracked: "\033[31m",
	unmerged:  "\033[31m",
	ignored:   "\033[90m",
}

func main() {
	opts := parseOptions(os.Args[1:])
	a := &app{options: opts}
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

func parseOptions(args []string) options {
	var opts options
	for _, arg := range args {
		switch arg {
		case "--ignored":
			opts.ignored = true
		case "-h", "--help":
			fmt.Println("usage: igs [--ignored]")
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "unknown option: %s\n", arg)
			os.Exit(2)
		}
	}
	return opts
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

	for _, line := range a.headerLines() {
		write(line + "\n")
	}
	if a.upstream != "" {
		for _, line := range branchStatusLines(a.upstream, a.ahead, a.behind) {
			write(line + "\n")
		}
	}
	if a.stash > 0 {
		write(fmt.Sprintf("Your stash currently has %d %s.\n", a.stash, plural(a.stash, "entry", "entries")))
	}
	for _, line := range a.operationLines() {
		write(line + "\n")
	}

	unmergedItems := a.itemsIn(unmerged)
	unstagedItems := a.itemsIn(unstaged)
	untrackedItems := a.itemsIn(untracked)
	stagedItems := a.itemsIn(staged)
	ignoredItems := a.itemsIn(ignored)

	if len(a.items) == 0 {
		if a.initial || a.upstream != "" || a.stash > 0 || a.detached {
			write("\n")
		}
		if a.initial {
			write("nothing to commit (create/copy files and use \"git add\" to track)\n")
		} else {
			write("nothing to commit, working tree clean\n")
		}
		return b.String()
	}

	write("\n")
	if len(unmergedItems) > 0 {
		write(fmt.Sprintf("Unmerged paths (%d)\n", len(unmergedItems)))
		a.renderItems(&b, &line, unmerged)
		write("\n")
	}

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

	if len(ignoredItems) > 0 {
		write("\n")
		write(fmt.Sprintf("Ignored files (%d)\n", len(ignoredItems)))
		a.renderItems(&b, &line, ignored)
	}

	if a.err != "" {
		write("\n")
		write(fmt.Sprintf("%s\n", a.err))
	}

	return b.String()
}

func (a *app) operationLines() []string {
	switch a.operation {
	case operationMerge:
		return []string{
			"You have unmerged paths.",
			"  (fix conflicts and run \"git commit\")",
			"  (use \"git merge --abort\" to abort the merge)",
		}
	case operationRebase:
		return []string{
			"You are currently rebasing.",
			"  (fix conflicts and then run \"git rebase --continue\")",
			"  (use \"git rebase --abort\" to check out the original branch)",
		}
	case operationCherryPick:
		return []string{
			"You are currently cherry-picking.",
			"  (fix conflicts and run \"git cherry-pick --continue\")",
			"  (use \"git cherry-pick --abort\" to cancel the cherry-pick operation)",
		}
	case operationRevert:
		return []string{
			"You are currently reverting.",
			"  (fix conflicts and run \"git revert --continue\")",
			"  (use \"git revert --abort\" to cancel the revert operation)",
		}
	default:
		return nil
	}
}

func (a *app) headerLines() []string {
	if a.detached {
		at := a.oid
		if len(at) > 7 {
			at = at[:7]
		}
		if at == "" {
			return []string{"HEAD detached"}
		}
		return []string{fmt.Sprintf("HEAD detached at %s", at)}
	}

	branch := a.branch
	if branch == "" {
		branch = "HEAD"
	}
	lines := []string{fmt.Sprintf("On branch %s", branch)}
	if a.initial {
		lines = append(lines, "", "No commits yet")
	}
	return lines
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
	branch, oid, initial, detached, upstream, ahead, behind, stash, operation, items, err := statusState(a.options)
	if err != nil {
		a.err = err.Error()
		return
	}

	a.branch = branch
	a.oid = oid
	a.initial = initial
	a.detached = detached
	a.upstream = upstream
	a.ahead = ahead
	a.behind = behind
	a.stash = stash
	a.operation = operation
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

	sections := []area{unmerged, untracked, unstaged, staged, ignored}
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
	if a.items[a.cursor].area == unmerged || a.items[a.cursor].area == ignored {
		return
	}
	if a.items[a.cursor].area == staged {
		a.unstage()
	} else {
		a.stage()
	}
}

func (a *app) stage() {
	if len(a.items) == 0 || a.items[a.cursor].area == staged || a.items[a.cursor].area == unmerged || a.items[a.cursor].area == ignored {
		return
	}
	a.runKeepingSection(a.items[a.cursor].area, "add", "--", a.items[a.cursor].path)
}

func (a *app) unstage() {
	if len(a.items) == 0 || a.items[a.cursor].area != staged || a.items[a.cursor].area == unmerged {
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
	case unmerged:
		return []area{untracked, unstaged, staged}
	case untracked:
		return []area{unstaged, staged, unmerged}
	case unstaged:
		return []area{untracked, staged, unmerged}
	case staged:
		return []area{unstaged, untracked, unmerged, ignored}
	case ignored:
		return []area{untracked, unstaged, staged, unmerged}
	default:
		return []area{unmerged, untracked, unstaged, staged, ignored}
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

func statusState(opts options) (string, string, bool, bool, string, int, int, int, operation, []item, error) {
	args := []string{"status", "--porcelain=v2", "--branch", "--show-stash", "--untracked-files=normal"}
	if opts.ignored {
		args = append(args, "--ignored=matching")
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if strings.Contains(message, "not a git repository") {
			message = "not a git repository"
		}
		if message == "" {
			message = err.Error()
		}
		return "", "", false, false, "", 0, 0, 0, operationNone, nil, fmt.Errorf("%s", message)
	}

	var branch string
	var oid string
	var initial bool
	var detached bool
	var upstream string
	var ahead int
	var behind int
	var stash int
	var items []item
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			parseHeader(line, &branch, &oid, &initial, &detached, &upstream, &ahead, &behind, &stash)
			continue
		}

		switch line[0] {
		case '!':
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				items = append(items, item{area: ignored, status: "!!", path: fields[1]})
			}
		case '?':
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				items = append(items, item{area: untracked, status: "??", path: fields[1]})
			}
		case 'u':
			if parsed, ok := parseUnmergedLine(line); ok {
				items = append(items, parsed)
			}
		case '1', '2':
			parsed := parseChangedLine(line)
			items = append(items, parsed...)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return areaOrder(items[i].area) < areaOrder(items[j].area)
	})

	return branch, oid, initial, detached, upstream, ahead, behind, stash, currentOperation(), items, nil
}

func currentOperation() operation {
	switch {
	case gitPathExists("rebase-merge"), gitPathExists("rebase-apply"):
		return operationRebase
	case gitPathExists("CHERRY_PICK_HEAD"):
		return operationCherryPick
	case gitPathExists("REVERT_HEAD"):
		return operationRevert
	case gitPathExists("MERGE_HEAD"):
		return operationMerge
	default:
		return operationNone
	}
}

func gitPathExists(path string) bool {
	resolved := strings.TrimSpace(gitOutput("rev-parse", "--git-path", path))
	if resolved == "" {
		return false
	}
	_, err := os.Stat(resolved)
	return err == nil
}

func gitOutput(args ...string) string {
	out, _ := exec.Command("git", args...).Output()
	return string(out)
}

func parseHeader(line string, branch, oid *string, initial, detached *bool, upstream *string, ahead, behind, stash *int) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return
	}

	switch fields[1] {
	case "branch.oid":
		if fields[2] == "(initial)" {
			*initial = true
		} else {
			*oid = fields[2]
		}
	case "branch.head":
		if fields[2] == "(detached)" {
			*detached = true
		} else {
			*branch = fields[2]
		}
	case "branch.upstream":
		*upstream = fields[2]
	case "branch.ab":
		if len(fields) >= 4 {
			*ahead = parseSignedCount(fields[2])
			*behind = parseSignedCount(fields[3])
		}
	case "stash":
		*stash = parseSignedCount(fields[2])
	}
}

func parseSignedCount(value string) int {
	value = strings.TrimPrefix(value, "+")
	value = strings.TrimPrefix(value, "-")
	var n int
	_, _ = fmt.Sscanf(value, "%d", &n)
	return n
}

func parseUnmergedLine(line string) (item, bool) {
	fields := strings.Fields(line)
	if len(fields) < 11 {
		return item{}, false
	}
	return item{area: unmerged, status: fields[1], path: fields[len(fields)-1]}, true
}

func parseChangedLine(line string) []item {
	fields := strings.Fields(line)
	if len(fields) < 9 {
		return nil
	}

	xy := fields[1]
	submodule := fields[2]
	path := fields[len(fields)-1]
	if line[0] == '2' && len(fields) >= 10 {
		path = fields[len(fields)-2]
	}
	detail := submoduleDetail(submodule)

	var items []item
	if len(xy) >= 2 {
		if xy[1] != '.' {
			items = append(items, item{area: unstaged, status: string(xy[1]), path: path, detail: detail})
		}
		if xy[0] != '.' {
			items = append(items, item{area: staged, status: string(xy[0]), path: path, detail: detail})
		}
	}
	return items
}

func submoduleDetail(submodule string) string {
	if len(submodule) < 4 || submodule[0] != 'S' {
		return ""
	}

	var details []string
	if submodule[1] == 'C' {
		details = append(details, "new commits")
	}
	if submodule[2] == 'M' {
		details = append(details, "modified content")
	}
	if submodule[3] == 'U' {
		details = append(details, "untracked content")
	}
	if len(details) == 0 {
		return ""
	}
	return "(" + strings.Join(details, ", ") + ")"
}

func areaOrder(which area) int {
	switch which {
	case unmerged:
		return 0
	case untracked:
		return 1
	case unstaged:
		return 2
	case staged:
		return 3
	case ignored:
		return 4
	default:
		return 3
	}
}

func colorStatus(it item) string {
	return colorize(it, statusName(it.status))
}

func colorPath(it item) string {
	path := it.path
	if it.detail != "" {
		path += " " + it.detail
	}
	return colorize(it, path)
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
	case unmerged:
		color = gitColors.unmerged
	case ignored:
		color = gitColors.ignored
	}

	if color == "" {
		return text
	}
	return color + text + "\033[m"
}

func branchStatusLines(upstream string, ahead, behind int) []string {
	switch {
	case ahead == 0 && behind == 0:
		return []string{fmt.Sprintf("Your branch is up to date with '%s'.", upstream)}
	case ahead > 0 && behind == 0:
		return []string{fmt.Sprintf("Your branch is ahead of '%s' by %d %s.", upstream, ahead, plural(ahead, "commit", "commits"))}
	case ahead == 0 && behind > 0:
		return []string{fmt.Sprintf("Your branch is behind '%s' by %d %s.", upstream, behind, plural(behind, "commit", "commits"))}
	default:
		return []string{fmt.Sprintf("Your branch and '%s' have diverged,", upstream), fmt.Sprintf("and have %d and %d different commits each, respectively.", ahead, behind)}
	}
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func statusName(s string) string {
	switch s {
	case "DD":
		return "both deleted:"
	case "AU":
		return "added by us:"
	case "UD":
		return "deleted by them:"
	case "UA":
		return "added by them:"
	case "DU":
		return "deleted by us:"
	case "AA":
		return "both added:"
	case "UU":
		return "both modified:"
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
	case "??", "!!":
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
