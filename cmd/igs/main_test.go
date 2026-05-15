package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitialCleanRepoRendersLikeGitStatus(t *testing.T) {
	withRepoNoCommit(t, func(repo string) {
		a := loadApp(t)
		got := plain(a.render())
		want := "On branch master\n\nNo commits yet\n\nnothing to commit (create/copy files and use \"git add\" to track)\n"
		if got != want {
			t.Fatalf("render mismatch\nwant:\n%q\ngot:\n%q", want, got)
		}
	})
}

func TestCleanRepoRendersLikeGitStatus(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, "README.md", "hello\n")
		git(t, repo, "add", "README.md")
		git(t, repo, "commit", "-m", "init")

		a := loadApp(t)
		got := plain(a.render())
		want := "On branch master\nnothing to commit, working tree clean\n"
		if got != want {
			t.Fatalf("render mismatch\nwant:\n%q\ngot:\n%q", want, got)
		}
	})
}

func TestPathsWithSpacesAreParsed(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, "tracked file.txt", "one\n")
		git(t, repo, "add", "tracked file.txt")
		git(t, repo, "commit", "-m", "init")

		writeFile(t, repo, "tracked file.txt", "two\n")
		writeFile(t, repo, "untracked file.txt", "new\n")
		git(t, repo, "mv", "tracked file.txt", "renamed file.txt")

		a := loadApp(t)
		assertHasItem(t, a.items, untracked, "??", "untracked file.txt")
		assertHasItem(t, a.items, unstaged, "M", "renamed file.txt")
		assertHasItem(t, a.items, staged, "R", "renamed file.txt")
		for _, item := range a.items {
			if item.path == "renamed file.txt" && item.origPath != "tracked file.txt" {
				t.Fatalf("rename orig path mismatch: %#v", item)
			}
		}
	})
}

func TestChangedFilesAreParsed(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, "tracked.txt", "one\n")
		git(t, repo, "add", "tracked.txt")
		git(t, repo, "commit", "-m", "init")

		writeFile(t, repo, "tracked.txt", "two\n")
		writeFile(t, repo, "untracked.txt", "new\n")
		writeFile(t, repo, "staged.txt", "staged\n")
		git(t, repo, "add", "staged.txt")

		a := loadApp(t)
		assertHasItem(t, a.items, untracked, "??", "untracked.txt")
		assertHasItem(t, a.items, unstaged, "M", "tracked.txt")
		assertHasItem(t, a.items, staged, "A", "staged.txt")
	})
}

func TestAheadBehindAndDivergedStatus(t *testing.T) {
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	git(t, base, "init", "--bare", origin)

	repo := filepath.Join(base, "repo")
	git(t, base, "clone", origin, repo)
	configureRepo(t, repo)
	writeFile(t, repo, "file.txt", "one\n")
	git(t, repo, "add", "file.txt")
	git(t, repo, "commit", "-m", "init")
	git(t, repo, "push", "-u", "origin", "master")

	withDir(t, repo, func() {
		writeFile(t, repo, "ahead.txt", "ahead\n")
		git(t, repo, "add", "ahead.txt")
		git(t, repo, "commit", "-m", "ahead")
		a := loadApp(t)
		if a.ahead != 1 || a.behind != 0 || a.upstream != "origin/master" {
			t.Fatalf("ahead state mismatch: upstream=%q ahead=%d behind=%d", a.upstream, a.ahead, a.behind)
		}
	})

	other := filepath.Join(base, "other")
	git(t, base, "clone", origin, other)
	configureRepo(t, other)
	writeFile(t, other, "behind.txt", "behind\n")
	git(t, other, "add", "behind.txt")
	git(t, other, "commit", "-m", "behind")
	git(t, other, "push")

	withDir(t, repo, func() {
		git(t, repo, "fetch", "origin")
		a := loadApp(t)
		if a.ahead != 1 || a.behind != 1 {
			t.Fatalf("diverged state mismatch: ahead=%d behind=%d", a.ahead, a.behind)
		}
		got := plain(a.render())
		if !strings.Contains(got, "Your branch and 'origin/master' have diverged,") {
			t.Fatalf("missing diverged message:\n%s", got)
		}
	})
}

func TestStashCount(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, "file.txt", "one\n")
		git(t, repo, "add", "file.txt")
		git(t, repo, "commit", "-m", "init")
		writeFile(t, repo, "file.txt", "two\n")
		git(t, repo, "stash", "push", "-m", "test")

		git(t, repo, "config", "status.showStash", "true")
		a := loadApp(t)
		if a.stash != 1 {
			t.Fatalf("stash count mismatch: got %d", a.stash)
		}
		if !strings.Contains(plain(a.render()), "Your stash currently has 1 entry.") {
			t.Fatalf("missing stash message:\n%s", plain(a.render()))
		}
	})
}

func TestNoCommitsYetWithUntrackedFile(t *testing.T) {
	withRepoNoCommit(t, func(repo string) {
		writeFile(t, repo, "new.txt", "new\n")
		a := loadApp(t)
		if a.branch != "master" || !a.initial {
			t.Fatalf("initial branch mismatch: branch=%q initial=%v", a.branch, a.initial)
		}
		got := plain(a.render())
		if !strings.Contains(got, "On branch master\n\nNo commits yet") || !strings.Contains(got, "Untracked files (1)") {
			t.Fatalf("missing initial status lines:\n%s", got)
		}
	})
}

func TestDetachedHead(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, "file.txt", "one\n")
		git(t, repo, "add", "file.txt")
		git(t, repo, "commit", "-m", "init")
		sha := strings.TrimSpace(gitOutputForTest(t, repo, "rev-parse", "--short", "HEAD"))
		git(t, repo, "checkout", "--detach", "HEAD")

		a := loadApp(t)
		if !a.detached {
			t.Fatalf("expected detached head")
		}
		got := plain(a.render())
		if !strings.Contains(got, "HEAD detached at "+sha) {
			t.Fatalf("missing detached line:\n%s", got)
		}
	})
}

func TestMergeConflictIsParsed(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, "conflict.txt", "base\n")
		git(t, repo, "add", "conflict.txt")
		git(t, repo, "commit", "-m", "init")
		git(t, repo, "checkout", "-b", "other")
		writeFile(t, repo, "conflict.txt", "other\n")
		git(t, repo, "commit", "-am", "other")
		git(t, repo, "checkout", "master")
		writeFile(t, repo, "conflict.txt", "master\n")
		git(t, repo, "commit", "-am", "master")

		cmd := exec.Command("git", "merge", "other")
		cmd.Dir = repo
		_ = cmd.Run()

		a := loadApp(t)
		if len(a.items) == 0 {
			t.Fatalf("expected conflict item")
		}
		assertHasItem(t, a.items, unmerged, "UU", "conflict.txt")
		if got := plain(a.render()); !strings.Contains(got, "Unmerged paths (1)") || !strings.Contains(got, "both modified:") {
			t.Fatalf("missing unmerged render:\n%s", got)
		}
	})
}

func TestIgnoredFilesOption(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, ".gitignore", "ignored.txt\n")
		writeFile(t, repo, "ignored.txt", "ignored\n")
		git(t, repo, "add", ".gitignore")
		git(t, repo, "commit", "-m", "ignore")

		withoutIgnored := loadApp(t)
		for _, item := range withoutIgnored.items {
			if item.area == ignored {
				t.Fatalf("did not expect ignored item without option: %#v", item)
			}
		}

		withIgnored := loadAppWithOptions(t, options{ignored: true})
		assertHasItem(t, withIgnored.items, ignored, "!!", "ignored.txt")
		if got := plain(withIgnored.render()); !strings.Contains(got, "Ignored files (1)") {
			t.Fatalf("missing ignored section:\n%s", got)
		}
	})
}

func TestShowStashOption(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, "file.txt", "one\n")
		git(t, repo, "add", "file.txt")
		git(t, repo, "commit", "-m", "init")
		writeFile(t, repo, "file.txt", "two\n")
		git(t, repo, "stash", "push", "-m", "test")

		withoutOption := loadApp(t)
		if withoutOption.stash != 0 {
			t.Fatalf("stash should be hidden by default, got %d", withoutOption.stash)
		}

		withOption := loadAppWithOptions(t, options{showStash: true})
		if withOption.stash != 1 {
			t.Fatalf("stash count mismatch: got %d", withOption.stash)
		}
		if !strings.Contains(plain(withOption.render()), "Your stash currently has 1 entry.") {
			t.Fatalf("missing stash message:\n%s", plain(withOption.render()))
		}
	})
}

func TestUpstreamGone(t *testing.T) {
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	git(t, base, "init", "--bare", origin)

	repo := filepath.Join(base, "repo")
	git(t, base, "clone", origin, repo)
	configureRepo(t, repo)
	writeFile(t, repo, "file.txt", "one\n")
	git(t, repo, "add", "file.txt")
	git(t, repo, "commit", "-m", "init")
	git(t, repo, "push", "-u", "origin", "master")
	git(t, origin, "update-ref", "-d", "refs/heads/master")
	git(t, repo, "fetch", "--prune", "origin")

	withDir(t, repo, func() {
		a := loadApp(t)
		if !a.gone {
			t.Fatalf("expected gone upstream")
		}
		got := plain(a.render())
		if !strings.Contains(got, "Your branch is based on 'origin/master', but the upstream is gone.") {
			t.Fatalf("missing gone upstream message:\n%s", got)
		}
	})
}

func TestSparseCheckoutStatus(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, "a/a.txt", "a\n")
		writeFile(t, repo, "b/b.txt", "b\n")
		git(t, repo, "add", ".")
		git(t, repo, "commit", "-m", "init")
		git(t, repo, "sparse-checkout", "init", "--cone")
		git(t, repo, "sparse-checkout", "set", "a")

		a := loadApp(t)
		if a.sparse == "" {
			t.Fatalf("expected sparse checkout status")
		}
		if !strings.Contains(plain(a.render()), "You are in a sparse checkout") {
			t.Fatalf("missing sparse checkout message:\n%s", plain(a.render()))
		}
	})
}

func TestShortInteractiveRender(t *testing.T) {
	withRepo(t, func(repo string) {
		writeFile(t, repo, "tracked.txt", "one\n")
		git(t, repo, "add", "tracked.txt")
		git(t, repo, "commit", "-m", "init")
		writeFile(t, repo, "tracked.txt", "two\n")
		writeFile(t, repo, "staged.txt", "staged\n")
		git(t, repo, "add", "staged.txt")

		a := loadAppWithOptions(t, options{short: true})
		got := plain(a.render())
		if !strings.Contains(got, "Unstaged\n") || !strings.Contains(got, "Staged\n") {
			t.Fatalf("missing short sections:\n%s", got)
		}
		if strings.Contains(got, "Unstaged changes") || strings.Contains(got, "Staged changes") {
			t.Fatalf("short render contains long section names:\n%s", got)
		}
	})
}

func TestSequencerOperationStatuses(t *testing.T) {
	cases := []struct {
		name      string
		gitPath   string
		operation operation
		message   string
	}{
		{"rebase merge", "rebase-merge", operationRebase, "You are currently rebasing."},
		{"rebase apply", "rebase-apply", operationRebase, "You are currently rebasing."},
		{"cherry pick", "CHERRY_PICK_HEAD", operationCherryPick, "You are currently cherry-picking."},
		{"revert", "REVERT_HEAD", operationRevert, "You are currently reverting."},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			withRepo(t, func(repo string) {
				writeFile(t, repo, "file.txt", "one\n")
				git(t, repo, "add", "file.txt")
				git(t, repo, "commit", "-m", "init")
				writeGitPath(t, repo, tt.gitPath, "x\n")

				a := loadApp(t)
				if a.operation != tt.operation {
					t.Fatalf("operation mismatch: got %q want %q", a.operation, tt.operation)
				}
				if !strings.Contains(plain(a.render()), tt.message) {
					t.Fatalf("missing operation message:\n%s", plain(a.render()))
				}
			})
		})
	}
}

func TestSubmoduleDetail(t *testing.T) {
	cases := map[string]string{
		"S...": "",
		"SC..": "(new commits)",
		"S.M.": "(modified content)",
		"S..U": "(untracked content)",
		"SCMU": "(new commits, modified content, untracked content)",
	}
	for input, want := range cases {
		if got := submoduleDetail(input); got != want {
			t.Fatalf("submoduleDetail(%q) = %q, want %q", input, got, want)
		}
	}
}

func loadApp(t *testing.T) *app {
	t.Helper()
	return loadAppWithOptions(t, options{})
}

func loadAppWithOptions(t *testing.T, opts options) *app {
	t.Helper()
	a := &app{options: opts}
	a.refresh()
	if a.err != "" {
		t.Fatalf("refresh error: %s", a.err)
	}
	return a
}

func withRepo(t *testing.T, fn func(repo string)) {
	t.Helper()
	withRepoNoCommit(t, func(repo string) {
		fn(repo)
	})
}

func withRepoNoCommit(t *testing.T, fn func(repo string)) {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-b", "master")
	configureRepo(t, repo)
	withDir(t, repo, func() { fn(repo) })
}

func withDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()
	fn()
}

func configureRepo(t *testing.T, repo string) {
	t.Helper()
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = gitOutputForTest(t, dir, args...)
}

func gitOutputForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func writeFile(t *testing.T, repo, name, content string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeGitPath(t *testing.T, repo, name, content string) {
	t.Helper()
	path := strings.TrimSpace(gitOutputForTest(t, repo, "rev-parse", "--git-path", name))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(name, "/") || name == "rebase-merge" || name == "rebase-apply" {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertHasItem(t *testing.T, items []item, area area, status, path string) {
	t.Helper()
	for _, item := range items {
		if item.area == area && item.status == status && item.path == path {
			return
		}
	}
	t.Fatalf("missing item area=%v status=%q path=%q in %#v", area, status, path, items)
}

func plain(s string) string {
	s = strings.ReplaceAll(s, "\033[K", "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return s
}
