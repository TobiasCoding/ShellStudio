// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"shellstudio/internal/diagnostics"
	"shellstudio/internal/platform"
	"shellstudio/internal/store"
)

// Agent consoles (Claude Code and Codex) are resumable: while they run, a
// tracker records the conversation each one is in, and starting the console
// again (for example after the machine restarted) resumes that conversation
// instead of opening an empty one.

// AgentOf is the resumable agent of a console: "claude", "codex" or "".
func AgentOf(c store.Console) string {
	for _, id := range []string{c.Extension, executableName(c.Argv)} {
		if id == "claude" || id == "codex" {
			return id
		}
	}
	return ""
}

func executableName(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return filepath.Base(argv[0])
}

// agentHome is the configuration directory of an agent, honoring the
// console's own environment first.
func agentHome(agent string, env map[string]string) string {
	key, dir := "CLAUDE_CONFIG_DIR", ".claude"
	if agent == "codex" {
		key, dir = "CODEX_HOME", ".codex"
	}
	if v := env[key]; filepath.IsAbs(v) {
		return v
	}
	if v := os.Getenv(key); filepath.IsAbs(v) {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, dir)
}

var (
	conversationID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	codexRollout   = regexp.MustCompile(`/rollout-[^/]*-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.jsonl$`)
)

// procStart is field 22 of /proc/PID/stat: when the process started, in
// clock ticks since boot. It tells a live process from an earlier one that
// had the same PID before a reboot.
func procStart(root string, pid int) string {
	b, e := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if e != nil {
		return ""
	}
	s := string(b)
	// The command name (field 2) may contain spaces; it ends at the last ")".
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return ""
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return ""
	}
	return f[19]
}

// processTree is pid and its descendants, at most depth levels down: an
// agent may run behind a launcher process.
func processTree(root string, pid, depth int) []int {
	out := []int{pid}
	if depth == 0 {
		return out
	}
	tasks, _ := filepath.Glob(filepath.Join(root, strconv.Itoa(pid), "task", "*", "children"))
	for _, t := range tasks {
		b, _ := os.ReadFile(t)
		for _, f := range strings.Fields(string(b)) {
			if child, e := strconv.Atoi(f); e == nil {
				out = append(out, processTree(root, child, depth-1)...)
			}
		}
	}
	return out
}

// DetectConversation finds the conversation an agent process is in. Claude
// Code describes each running process in sessions/PID.json; Codex keeps its
// rollout file open. root is /proc except in tests.
func DetectConversation(root, agent string, pid int, env map[string]string) string {
	for _, p := range processTree(root, pid, 2) {
		switch agent {
		case "claude":
			b, e := os.ReadFile(filepath.Join(agentHome(agent, env), "sessions", strconv.Itoa(p)+".json"))
			if e != nil {
				continue
			}
			var s struct {
				PID       int    `json:"pid"`
				SessionID string `json:"sessionId"`
				ProcStart string `json:"procStart"`
			}
			if json.Unmarshal(b, &s) != nil || s.PID != p || !conversationID.MatchString(s.SessionID) {
				continue
			}
			// A file left behind by a process that had this PID before a
			// reboot describes another conversation.
			if s.ProcStart != "" && s.ProcStart != procStart(root, p) {
				continue
			}
			return s.SessionID
		case "codex":
			fds, _ := os.ReadDir(filepath.Join(root, strconv.Itoa(p), "fd"))
			for _, fd := range fds {
				target, e := os.Readlink(filepath.Join(root, strconv.Itoa(p), "fd", fd.Name()))
				if e != nil {
					continue
				}
				if m := codexRollout.FindStringSubmatch(target); m != nil {
					return m[1]
				}
			}
		}
	}
	return ""
}

// conversationExists reports whether the agent still has the transcript of
// a conversation: resuming a missing one would end the console at once.
func conversationExists(agent, id string, env map[string]string) bool {
	if !conversationID.MatchString(id) {
		return false
	}
	pattern := filepath.Join(agentHome(agent, env), "projects", "*", id+".jsonl")
	if agent == "codex" {
		pattern = filepath.Join(agentHome(agent, env), "sessions", "*", "*", "*", "rollout-*-"+id+".jsonl")
	}
	m, _ := filepath.Glob(pattern)
	return len(m) > 0
}

// resumeFlags are arguments that already choose a conversation.
var resumeFlags = map[string][]string{
	"claude": {"--resume", "-r", "--continue", "-c", "--session-id", "--fork-session"},
	"codex":  {"resume", "fork", "exec", "e"},
}

// ResumeArgv is the command line that starts a console: its own, or the one
// that resumes its recorded conversation when the agent still has it.
func ResumeArgv(c store.Console, conv store.Conversation, found bool) []string {
	agent := AgentOf(c)
	if agent == "" || !found || conv.Agent != agent || !conversationExists(agent, conv.ID, c.Env) {
		return c.Argv
	}
	for _, a := range c.Argv[1:] {
		for _, f := range resumeFlags[agent] {
			if a == f || strings.HasPrefix(a, f+"=") {
				return c.Argv
			}
		}
	}
	if agent == "codex" {
		// codex resume ID takes the same options (sandbox, approvals) as
		// codex itself; after the subcommand they apply to the session.
		return append([]string{c.Argv[0], "resume", conv.ID}, c.Argv[1:]...)
	}
	return append(append([]string{}, c.Argv...), "--resume", conv.ID)
}

// Resumable reports whether starting the console resumes a conversation.
func (a *App) Resumable(c store.Console) bool {
	conv, e := a.Store.Conversation(c.ID)
	return len(ResumeArgv(c, conv, e == nil)) != len(c.Argv)
}

// ExecArgv is ResumeArgv with the conversation stored for the console.
func (a *App) ExecArgv(c store.Console) []string {
	conv, e := a.Store.Conversation(c.ID)
	argv := ResumeArgv(c, conv, e == nil)
	if len(argv) != len(c.Argv) {
		a.Store.Event("console-resume", c.ID)
	}
	return argv
}

// TrackConversations records the conversation of every running agent
// console. It reports how many agent consoles are running.
func (a *App) TrackConversations() (int, error) {
	o, e := a.Mux.Run(false, "list-panes", "-a", "-F", "#{session_name}\t#{pane_pid}\t#{pane_dead}")
	if e != nil {
		return 0, e
	}
	running := 0
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		f := strings.Split(l, "\t")
		if len(f) != 3 || !strings.HasPrefix(f[0], "c-") || f[2] != "0" {
			continue
		}
		c, e := a.Store.Console(strings.TrimPrefix(f[0], "c-"))
		if e != nil {
			continue
		}
		agent := AgentOf(c)
		pid, _ := strconv.Atoi(f[1])
		if agent == "" || pid <= 0 {
			continue
		}
		running++
		if id := DetectConversation("/proc", agent, pid, c.Env); id != "" {
			if e := a.Store.SetConversation(c.ID, store.Conversation{Agent: agent, ID: id}); e != nil {
				return running, e
			}
		}
	}
	return running, nil
}

// TrackInterval is how often the tracker looks at the agent consoles.
const TrackInterval = 10 * time.Second

// Track is the tracker process: one per user (a lock), until no agent
// console has been running for a minute or the programs server ends.
func (a *App) Track() error {
	l, e := platform.Lock(filepath.Join(a.Paths.Runtime, "track.lock"))
	if e != nil {
		return nil // Another tracker is running.
	}
	defer platform.Unlock(l)
	idle := 0
	for idle < 6 {
		n, e := a.TrackConversations()
		if e != nil && !strings.Contains(e.Error(), "no server") {
			diagnostics.Record(diagnostics.Entry{Event: "track"}, e, time.Time{})
		}
		if n == 0 {
			idle++
		} else {
			idle = 0
		}
		time.Sleep(TrackInterval)
	}
	return nil
}

// EnsureTracker starts the tracker unless it is running. The tracker runs
// in its own session, detached from the caller: an intermediate process
// starts it and exits, so a caller that then becomes Claude or Codex
// (execve) never has a child it does not reap.
func (a *App) EnsureTracker() {
	l, e := platform.Lock(filepath.Join(a.Paths.Runtime, "track.lock"))
	if e != nil {
		return // Running.
	}
	platform.Unlock(l)
	c := exec.Command(a.Mux.Binary, "_track", "--spawn")
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	c.Run()
}

// SpawnTracker is the intermediate process of EnsureTracker.
func (a *App) SpawnTracker() error {
	c := exec.Command(a.Mux.Binary, "_track")
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c.Start()
}

// ensureTrackerFor starts the tracker when an agent console is running
// (state is "live"): consoles started by an earlier release have none.
func (a *App) ensureTrackerFor(cs []store.Console, state func(string) string) {
	for _, c := range cs {
		if state(c.ID) == "live" && AgentOf(c) != "" {
			a.EnsureTracker()
			return
		}
	}
}
