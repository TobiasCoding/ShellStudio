// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"shellstudio/internal/app"
	"shellstudio/internal/extensions"
	"shellstudio/internal/platform"
	"shellstudio/internal/runner"
	"shellstudio/internal/store"
	"shellstudio/internal/ui"
)

var version = "0.1.0"

func main() {
	syscall.Umask(0077)
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "ShellStudio:", e)
		os.Exit(1)
	}
}
func printJSON(v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	fmt.Println(string(b))
	return nil
}
func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version":
			fmt.Println("ShellStudio", version)
			return nil
		case "help", "--help", "-h":
			fmt.Print(help)
			return nil
		case "schema":
			b, e := extensions.Assets.ReadFile("schema.json")
			if e != nil {
				return e
			}
			fmt.Println(string(b))
			return nil
		case "validate":
			if len(args) != 2 {
				return errors.New("usage: shellstudio validate manifest.json")
			}
			m, e := extensions.ImportPreview(args[1])
			if e != nil {
				return e
			}
			fmt.Printf("Valid: %s %s (schema %d)\n", m.ID, m.Version, m.Schema)
			return nil
		case "_empty":
			fmt.Println("ShellStudio\r\nNo consoles in this view. Press F10 to return to the menu.")
			for {
				time.Sleep(time.Hour)
			}
		}
	}
	a, e := app.Open()
	if e != nil {
		return e
	}
	defer a.Close()
	if len(args) > 0 {
		switch args[0] {
		case "doctor":
			return doctor(a)
		case "backup":
			if len(args) != 2 {
				return errors.New("usage: shellstudio backup /absolute/path.db")
			}
			p, e := filepath.Abs(args[1])
			if e != nil {
				return e
			}
			if e = a.Store.Backup(p); e != nil {
				return e
			}
			s, e := store.Open(p)
			if e != nil {
				return e
			}
			defer s.Close()
			if e = s.Check(); e != nil {
				return e
			}
			fmt.Println("Verified backup:", p)
			return nil
		case "views":
			vs, e := a.Store.Views()
			if e != nil {
				return e
			}
			return printJSON(vs)
		case "consoles":
			cs, e := a.Store.Consoles("")
			if e != nil {
				return e
			}
			return printJSON(cs)
		case "_render":
			if len(args) != 3 {
				return errors.New("render needs a client ID and view ID")
			}
			v, e := a.Store.View(args[2])
			if e != nil {
				return e
			}
			cs, e := a.Store.Consoles(v.ID)
			if e != nil {
				return e
			}
			name, e := a.Mux.Sync(args[1], v, cs, 100, 30)
			if e != nil {
				return e
			}
			fmt.Println(name)
			return nil
		case "new-view":
			f := flag.NewFlagSet("new-view", flag.ContinueOnError)
			folder := f.String("folder", "", "base folder")
			if e = f.Parse(args[1:]); e != nil {
				return e
			}
			if f.NArg() != 1 {
				return errors.New("usage: shellstudio new-view [--folder PATH] NAME")
			}
			p, e := platform.Directory(*folder, "", a.CWD)
			if e != nil {
				return e
			}
			v, e := a.Store.NewView(f.Arg(0), p)
			if e != nil {
				return e
			}
			return printJSON(v)
		case "launch":
			f := flag.NewFlagSet("launch", flag.ContinueOnError)
			view := f.String("view", "", "view ID")
			ext := f.String("extension", "terminal", "extension ID")
			profile := f.String("profile", "", "profile")
			folder := f.String("folder", "", "explicit folder")
			name := f.String("name", "", "console name")
			if e = f.Parse(args[1:]); e != nil {
				return e
			}
			if *view == "" {
				return errors.New("launch requires --view ID")
			}
			c, e := a.Launch(*view, *name, *ext, *profile, *folder)
			if e != nil {
				return e
			}
			return printJSON(c)
		case "restart", "stop":
			if len(args) != 2 {
				return errors.New("usage: shellstudio " + args[0] + " CONSOLE_ID")
			}
			if args[0] == "restart" {
				return a.Restart(args[1])
			}
			return a.Stop(args[1])
		case "_exec":
			if len(args) != 2 {
				return errors.New("invalid console invocation")
			}
			c, e := a.Store.Console(args[1])
			if e != nil {
				return e
			}
			if len(c.Argv) == 0 {
				return errors.New("missing console executable")
			}
			p, e := exec.LookPath(c.Argv[0])
			if e != nil {
				return e
			}
			if e = os.Chdir(c.Folder); e != nil {
				return e
			}
			a.Close()
			return syscall.Exec(p, c.Argv, runner.Env(c.Env))
		case "_relay":
			if len(args) != 2 {
				return errors.New("invalid relay")
			}
			states, e := a.Mux.Programs()
			if e != nil {
				return e
			}
			if !states[args[1]] {
				fmt.Println("Stopped console. Press F10 and use r to restart explicitly.")
				return nil
			}
			tm, e := exec.LookPath("tmux")
			if e != nil {
				return e
			}
			sock := a.Mux.Socket(false)
			a.Close()
			return syscall.Exec(tm, []string{"tmux", "-S", sock, "attach-session", "-t", "=c-" + args[1]}, runner.Env(nil))
		case "extensions":
			if len(args) == 1 {
				return printJSON(extensions.Catalog(a.Paths.Config))
			}
			return errors.New("use the Extensions TUI screen to review installation actions")
		case "notes", "viewer", "_explorer":
		default:
			return fmt.Errorf("unknown command %q; use --help", args[0])
		}
	}
	screen := "views"
	if len(args) > 0 && args[0] == "notes" {
		screen = "notes"
	}
	model := ui.New(a, screen)
	defer a.Mux.Cleanup(model.Client)
	if len(args) > 0 && args[0] == "viewer" {
		if len(args) < 2 || len(args) > 3 {
			return errors.New("usage: shellstudio viewer agent-chat|agent-gantt [DATABASE]")
		}
		kind := args[1]
		if kind != "agent-chat" && kind != "agent-gantt" {
			return errors.New("unknown viewer")
		}
		file := "messages.sqlite3"
		if kind == "agent-gantt" {
			file = "projects.sqlite3"
		}
		p := filepath.Join(a.Paths.Data, "extension-data", kind, file)
		if len(args) == 3 {
			p, e = filepath.Abs(args[2])
			if e != nil {
				return e
			}
		}
		model.SetViewer(kind, p)
	}
	if len(args) == 2 && args[0] == "_explorer" {
		model.SetExplorer(args[1])
	}
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(sig)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sig:
			p.Send(ui.ExitMsg{})
		case <-done:
		}
	}()
	_, e = p.Run()
	return e
}
func doctor(a *app.App) error {
	failed := false
	report := func(label string, e error) {
		if e != nil {
			failed = true
			fmt.Printf("FAIL %-18s %s\n", label, e)
		} else {
			fmt.Printf("OK   %s\n", label)
		}
	}
	_, e := exec.LookPath("tmux")
	if e == nil {
		var s string
		s, e = runner.Run(5*time.Second, "", nil, "tmux", "-V")
		fmt.Print(strings.TrimSpace(s) + "\n")
	}
	report("tmux", e)
	report("database", a.Store.Check())
	_, e = platform.LoadConfig(a.Paths.Config)
	report("configuration", e)
	for _, d := range []string{a.Paths.Config, a.Paths.Data, a.Paths.State, a.Paths.Runtime} {
		fi, e := os.Lstat(d)
		if e == nil && fi.Mode().Perm() != 0700 {
			e = errors.New("expected permissions 0700")
		}
		report(d, e)
	}
	for _, entry := range extensions.Catalog(a.Paths.Config) {
		if entry.Error != "" {
			fmt.Println("WARN manifest:", entry.Error)
			continue
		}
		p, e := a.Extensions.Detect(entry.Manifest)
		if e != nil {
			fmt.Printf("INFO %-18s not installed\n", entry.Manifest.ID)
		} else {
			fmt.Printf("INFO %-18s detected: %s\n", entry.Manifest.ID, p)
		}
	}
	states, e := a.Mux.Programs()
	report("console server", e)
	fmt.Printf("INFO %d known tmux programs; stopped metadata is never relaunched automatically\n", len(states))
	if failed {
		return errors.New("diagnostics found failures")
	}
	return nil
}

const help = `ShellStudio — persistent consoles, views, extensions and global notes

Usage:
  shellstudio                         Open the TUI
  shellstudio notes                   Open global Notes
  shellstudio viewer KIND [DATABASE]  Read-only agent-chat / agent-gantt viewer
  shellstudio doctor                  Check storage, dependencies and extensions
  shellstudio validate MANIFEST       Validate a file or HTTPS manifest
  shellstudio schema                  Print the public JSON schema
  shellstudio backup DESTINATION      Create and verify a SQLite backup
  shellstudio new-view [--folder PATH] NAME
  shellstudio views | consoles | extensions
  shellstudio launch --view ID [--extension ID] [--profile NAME] [--folder PATH]
  shellstudio restart CONSOLE_ID       Explicitly restart a stopped program
  shellstudio stop CONSOLE_ID          End a program in all views
  shellstudio version

TUI: Tab changes section; ? shows SSH help; F10 leaves a tmux view.
Closing the UI preserves programs. Restarting the computer stops them.
Storage follows XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME and XDG_RUNTIME_DIR.
ShellStudio is GPL-3.0-only; bundled MCP servers retain their MIT licenses.
`
