// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"shellstudio/internal/store"
)

const convID = "f8dfa890-5881-4003-b8d3-6bdbffe04e44"

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}

// stat is /proc/PID/stat with start time 555 (field 22); the name has a space.
func stat(pid string) string {
	return pid + " (claude code) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 555 19 20"
}

func TestDetectClaudeConversationChecksTheProcessStart(t *testing.T) {
	proc, home := t.TempDir(), t.TempDir()
	env := map[string]string{"CLAUDE_CONFIG_DIR": home}
	writeFile(t, filepath.Join(proc, "123", "stat"), stat("123"))
	writeFile(t, filepath.Join(home, "sessions", "123.json"), `{"pid":123,"sessionId":"`+convID+`","procStart":"555"}`)
	if got := DetectConversation(proc, "claude", 123, env); got != convID {
		t.Fatalf("got %q", got)
	}
	// A file left by an earlier process with the same PID (before a reboot).
	writeFile(t, filepath.Join(home, "sessions", "123.json"), `{"pid":123,"sessionId":"`+convID+`","procStart":"999"}`)
	if got := DetectConversation(proc, "claude", 123, env); got != "" {
		t.Fatalf("stale file trusted: %q", got)
	}
	// Behind a launcher: the agent is a child of the pane's process.
	writeFile(t, filepath.Join(proc, "100", "task", "100", "children"), "123 ")
	writeFile(t, filepath.Join(home, "sessions", "123.json"), `{"pid":123,"sessionId":"`+convID+`","procStart":"555"}`)
	if got := DetectConversation(proc, "claude", 100, env); got != convID {
		t.Fatalf("child: %q", got)
	}
}

func TestDetectCodexConversationFromItsOpenRollout(t *testing.T) {
	proc := t.TempDir()
	fd := filepath.Join(proc, "77", "fd")
	os.MkdirAll(fd, 0700)
	os.Symlink("/srv/agent/.codex/logs_2.sqlite", filepath.Join(fd, "3"))
	os.Symlink("/srv/agent/.codex/sessions/2026/10/28/rollout-2026-10-28T11-47-54-"+convID+".jsonl", filepath.Join(fd, "9"))
	if got := DetectConversation(proc, "codex", 77, nil); got != convID {
		t.Fatalf("got %q", got)
	}
	if got := DetectConversation(proc, "codex", 78, nil); got != "" {
		t.Fatalf("missing process: %q", got)
	}
}

func TestResumeArgvOnlyResumesExistingConversations(t *testing.T) {
	claudeHome, codexHome := t.TempDir(), t.TempDir()
	env := map[string]string{"CLAUDE_CONFIG_DIR": claudeHome, "CODEX_HOME": codexHome}
	claude := store.Console{Extension: "claude", Argv: []string{"claude", "--permission-mode", "auto"}, Env: env}
	conv := store.Conversation{Agent: "claude", ID: convID}
	if got := ResumeArgv(claude, conv, true); !reflect.DeepEqual(got, claude.Argv) {
		t.Fatalf("missing transcript resumed: %q", got)
	}
	writeFile(t, filepath.Join(claudeHome, "projects", "-opt-x", convID+".jsonl"), "{}\n")
	if got := ResumeArgv(claude, conv, true); strings.Join(got, " ") != "claude --permission-mode auto --resume "+convID {
		t.Fatalf("got %q", got)
	}
	if got := ResumeArgv(claude, conv, false); !reflect.DeepEqual(got, claude.Argv) {
		t.Fatalf("no conversation: %q", got)
	}
	own := claude
	own.Argv = []string{"claude", "--continue"}
	if got := ResumeArgv(own, conv, true); !reflect.DeepEqual(got, own.Argv) {
		t.Fatalf("profile choice overridden: %q", got)
	}
	codex := store.Console{Extension: "codex", Argv: []string{"codex", "--sandbox", "workspace-write"}, Env: env}
	cconv := store.Conversation{Agent: "codex", ID: convID}
	writeFile(t, filepath.Join(codexHome, "sessions", "2026", "11", "13", "rollout-2026-11-13T10-00-00-"+convID+".jsonl"), "{}\n")
	if got := ResumeArgv(codex, cconv, true); strings.Join(got, " ") != "codex resume "+convID+" --sandbox workspace-write" {
		t.Fatalf("got %q", got)
	}
	// A conversation of another agent, or a console that is not an agent.
	if got := ResumeArgv(codex, conv, true); !reflect.DeepEqual(got, codex.Argv) {
		t.Fatalf("wrong agent: %q", got)
	}
	term := store.Console{Extension: "terminal", Argv: []string{"/bin/bash"}}
	if got := ResumeArgv(term, conv, true); !reflect.DeepEqual(got, term.Argv) {
		t.Fatal(got)
	}
}

func TestConversationsAreStoredPerConsole(t *testing.T) {
	a, _ := offlineApp(t)
	v, _ := a.Store.NewView("Main", "/")
	addConsoles(t, a, v.ID, "a")
	if _, e := a.Store.Conversation("a"); e == nil {
		t.Fatal("conversation before any")
	}
	if e := a.Store.SetConversation("a", store.Conversation{Agent: "claude", ID: convID}); e != nil {
		t.Fatal(e)
	}
	if c, e := a.Store.Conversation("a"); e != nil || c.ID != convID || c.Agent != "claude" || c.Updated.IsZero() {
		t.Fatal(c, e)
	}
	a.Store.DeleteConsole("a")
	if _, e := a.Store.Conversation("a"); e == nil {
		t.Fatal("kept after the console was deleted")
	}
}
