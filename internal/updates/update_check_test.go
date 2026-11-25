package updates

import (
	"context"
	"strings"
	"testing"
)

// answer is one scripted git answer: a command prefix and its stdout.
type answer struct {
	prefix string
	stdout string
}

// fakeGit is the scripted git of test/unit/update_check.test.js: it answers by
// the first matching prefix of the joined arguments and fails otherwise.
func fakeGit(answers ...answer) GitRunner {
	return func(_ context.Context, args []string) Result {
		key := strings.Join(args, " ")
		for _, a := range answers {
			if strings.HasPrefix(key, a.prefix) {
				return Result{Status: 0, Stdout: a.stdout}
			}
		}
		return Result{Status: 1}
	}
}

// TestStableFollowsMainBetaFollowsBeta is the first case of the JS test.
func TestStableFollowsMainBetaFollowsBeta(t *testing.T) {
	run := fakeGit(
		answer{"rev-parse --verify --quiet refs/remotes/origin/main", ""},
		answer{"rev-parse --verify --quiet refs/remotes/origin/beta", ""},
	)
	branch, err := ResolvePreferredBranch(context.Background(), "stable", run, nil)
	if err != nil || branch != "main" {
		t.Fatalf("stable branch = %q, %v", branch, err)
	}
	branch, err = ResolvePreferredBranch(context.Background(), "beta", run, nil)
	if err != nil || branch != "beta" {
		t.Fatalf("beta branch = %q, %v", branch, err)
	}
}

// TestStableFallsBackToBeta is the second case: stable falls back only while
// main does not exist, and says so.
func TestStableFallsBackToBeta(t *testing.T) {
	run := fakeGit(answer{"rev-parse --verify --quiet refs/remotes/origin/beta", ""})
	var notes []string
	branch, err := ResolvePreferredBranch(context.Background(), "stable", run, func(note string) {
		notes = append(notes, note)
	})
	if err != nil || branch != "beta" {
		t.Fatalf("branch = %q, %v", branch, err)
	}
	if len(notes) != 1 {
		t.Fatalf("notes = %v", notes)
	}
	if !strings.Contains(notes[0], "origin/main is missing") {
		t.Fatalf("note = %q", notes[0])
	}
}

// TestNoBranchAtAll is the error path at the end of resolvePreferredBranch.
func TestNoBranchAtAll(t *testing.T) {
	if _, err := ResolvePreferredBranch(context.Background(), "stable", fakeGit(), nil); err == nil {
		t.Fatal("a repository without branches must be an error")
	}
}

// TestNewerVersionOnTheBranchIsReported is the third case: the version and the
// changes a person would call changes.
func TestNewerVersionOnTheBranchIsReported(t *testing.T) {
	run := fakeGit(
		answer{"rev-parse --verify --quiet refs/remotes/origin/beta", ""},
		answer{"rev-parse --abbrev-ref HEAD", "beta"},
		answer{"rev-parse HEAD", "aaa"},
		answer{"rev-list --count", "2"},
		answer{"show origin/beta:package.json", `{"version":"0.2.0"}`},
		answer{"show HEAD:package.json", `{"version":"0.1.0"}`},
		answer{"log --no-merges", "Add a thing\nchore: bump\nMerge branch 'x'"},
	)
	result := CheckForUpdate(context.Background(), run, CheckOptions{
		Channel:     "beta",
		FetchRemote: func(context.Context) bool { return true },
	})
	if !result.Checked {
		t.Fatalf("result = %+v", result)
	}
	if result.Update == nil {
		t.Fatal("no update reported")
	}
	if result.Update.Version == nil || *result.Update.Version != "0.2.0" {
		t.Fatalf("version = %v", result.Update.Version)
	}
	if result.Update.Branch != "beta" {
		t.Fatalf("branch = %q", result.Update.Branch)
	}
	if len(result.Update.Changes) != 1 || result.Update.Changes[0] != "Add a thing" {
		t.Fatalf("changes = %v", result.Update.Changes)
	}
	if result.Branch != "beta" {
		t.Fatalf("result branch = %q", result.Branch)
	}
}

// TestUnreachableRemoteIsNotAnAnswer is the fourth case.
func TestUnreachableRemoteIsNotAnAnswer(t *testing.T) {
	result := CheckForUpdate(context.Background(), fakeGit(), CheckOptions{
		Channel:     "beta",
		FetchRemote: func(context.Context) bool { return false },
	})
	if result.Checked || result.Update != nil || result.Reason != "offline" {
		t.Fatalf("result = %+v", result)
	}
}

// TestUpToDateCheckReportsTheBranch keeps the "checked, nothing waiting" answer.
func TestUpToDateCheckReportsTheBranch(t *testing.T) {
	run := fakeGit(
		answer{"rev-parse --verify --quiet refs/remotes/origin/beta", ""},
		answer{"rev-parse --abbrev-ref HEAD", "beta"},
		answer{"rev-parse HEAD", "aaa"},
		answer{"rev-list --count", "0"},
	)
	result := CheckForUpdate(context.Background(), run, CheckOptions{
		Channel:     "beta",
		FetchRemote: func(context.Context) bool { return true },
	})
	if !result.Checked || result.Update != nil || result.Branch != "beta" {
		t.Fatalf("result = %+v", result)
	}
}

// TestPendingUpdateRefusesAnOlderVersion keeps a channel switch from moving an
// install backwards.
func TestPendingUpdateRefusesAnOlderVersion(t *testing.T) {
	git := NewGit(fakeGit(
		answer{"rev-parse --abbrev-ref HEAD", "main"},
		answer{"rev-parse HEAD", "aaa"},
		answer{"rev-list --count", "3"},
		answer{"show origin/main:package.json", `{"version":"0.1.0"}`},
		answer{"show HEAD:package.json", `{"version":"0.2.0"}`},
	))
	if pending, ok := git.PendingUpdate(context.Background(), "main", false); ok {
		t.Fatalf("an older version counted as an update: %+v", pending)
	}
	if pending, ok := git.PendingUpdate(context.Background(), "main", true); !ok {
		t.Fatalf("an explicit switch must still see it: %+v", pending)
	}
}

// TestChangesAreCappedAndRestored covers the changelog limit and the restore
// path the CLI relies on.
func TestChangesAreCappedAndRestored(t *testing.T) {
	var subjects []string
	for i := 0; i < 12; i++ {
		subjects = append(subjects, "Change "+string(rune('a'+i)))
	}
	git := NewGit(fakeGit(answer{"log --no-merges", strings.Join(subjects, "\n")}))
	changes := git.ChangesBetween(context.Background(), "aaa", "bbb")
	if len(changes) != maxChanges {
		t.Fatalf("changes = %d, want %d", len(changes), maxChanges)
	}
	if changes[0] != "Change a" {
		t.Fatalf("first change = %q", changes[0])
	}

	restore := fakeGit(
		answer{"checkout --force beta", ""},
		answer{"reset --hard aaa", ""},
	)
	if !NewGit(restore).Restore(context.Background(), Head{SHA: "aaa", Branch: "beta"}) {
		t.Fatal("restore failed")
	}
}
