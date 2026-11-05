// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"shellstudio/internal/app"
	"shellstudio/internal/diagnostics"
	"shellstudio/internal/extensions"
	"shellstudio/internal/i18n"
	"shellstudio/internal/platform"
	"shellstudio/internal/runner"
	"shellstudio/internal/store"
	"shellstudio/internal/tui"
	"shellstudio/internal/ui"
)

var version = "dev"

func main() {
	syscall.Umask(0077)
	if e := loggedRun(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "ShellStudio:", e)
		os.Exit(1)
	}
}

func loggedRun(args []string) (err error) {
	command := diagnostics.Command(args)
	switch command {
	case "report", "version", "--version", "help", "--help", "-h", "schema":
		return run(args)
	}
	if e := diagnostics.Start(version); e != nil && !strings.HasPrefix(command, "_") {
		fmt.Fprintln(os.Stderr, "ShellStudio: diagnostic logging unavailable ("+diagnostics.Classify(e)+").")
	}
	started := time.Now()
	diagnostics.Record(diagnostics.Entry{Event: "command-start", Operation: command}, nil, time.Time{})
	defer func() {
		if p := recover(); p != nil {
			diagnostics.Record(diagnostics.Entry{Event: "panic", Operation: command}, errors.New("panic"), started)
			panic(p)
		}
		diagnostics.Record(diagnostics.Entry{Event: "command-end", Operation: command}, err, started)
	}()
	return run(args)
}
func printJSON(v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	fmt.Println(string(b))
	return nil
}

// flags parses the --name value options of the internal commands.
func flags(name string, args []string, names ...string) (map[string]*string, map[string]*bool, []string, error) {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	values, switches := map[string]*string{}, map[string]*bool{}
	for _, n := range names {
		if strings.HasPrefix(n, "!") {
			switches[n[1:]] = f.Bool(n[1:], false, "")
		} else {
			values[n] = f.String(n, "", "")
		}
	}
	e := f.Parse(args)
	return values, switches, f.Args(), e
}

func run(args []string) error {
	workspace, menu := false, false
	folder := ""
	if len(args) > 0 && args[0] == "--menu" {
		menu = true
		args = args[1:]
		if len(args) != 0 {
			return errors.New(i18n.T("cli.usage", "shellstudio --menu"))
		}
	}
	if !menu && (len(args) == 0 || args[0] == "open" || args[0] == "." || args[0] == ".." || strings.Contains(args[0], "/")) {
		workspace = true
		if len(args) > 0 && args[0] == "open" {
			args = args[1:]
		}
		if len(args) > 1 {
			return errors.New(i18n.T("cli.usage_open"))
		}
		if len(args) == 1 {
			folder = args[0]
		}
		args = nil
	}
	if !workspace && !menu && len(args) == 1 {
		if info, e := os.Stat(args[0]); e == nil && info.IsDir() && !reservedCommand(args[0]) {
			workspace = true
			folder = args[0]
			args = nil
		}
	}
	if len(args) > 0 {
		switch args[0] {
		case "report":
			return diagnosticReport(args[1:])
		case "update":
			if len(args) > 2 || (len(args) == 2 && args[1] != "--check") {
				return errors.New(i18n.T("cli.usage", "shellstudio update [--check]"))
			}
			return startupUpdate(true, len(args) == 2)
		case "version", "--version":
			fmt.Println("ShellStudio", version)
			return nil
		case "help", "--help", "-h":
			fmt.Print(i18n.T("cli.help"))
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
				return errors.New(i18n.T("cli.usage", "shellstudio validate manifest.json"))
			}
			m, e := extensions.ImportPreview(args[1])
			if e != nil {
				return e
			}
			fmt.Println(i18n.T("cli.valid", m.ID, m.Version, m.Schema))
			return nil
		}
	}
	if workspace || menu || (len(args) > 0 && args[0] == "notes") {
		if e := startupUpdate(false, false); e != nil {
			return e
		}
	}
	a, e := app.Open()
	if e != nil {
		return e
	}
	defer a.Close()
	if workspace || menu {
		var v store.View
		if menu {
			v, e = a.LastView()
		} else {
			v, e = a.Workspace(folder)
		}
		if e != nil {
			return e
		}
		return a.Open(v.ID, "")
	}
	switch args[0] {
	case "doctor":
		return doctor(a)
	case "backup":
		if len(args) != 2 {
			return errors.New(i18n.T("cli.usage", "shellstudio backup /absolute/path.db"))
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
		fmt.Println(i18n.T("cli.backup_ok", p))
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
	case "new-view":
		v, _, rest, e := flags("new-view", args[1:], "folder")
		if e != nil {
			return e
		}
		if len(rest) != 1 {
			return errors.New(i18n.T("cli.usage", "shellstudio new-view [--folder PATH] NAME"))
		}
		nv, e := a.NewView(rest[0], *v["folder"], nil, "")
		if e != nil {
			return e
		}
		return printJSON(nv)
	case "launch":
		v, _, _, e := flags("launch", args[1:], "view", "extension", "profile", "folder", "name")
		if e != nil {
			return e
		}
		if *v["view"] == "" {
			return errors.New(i18n.T("cli.launch_view"))
		}
		kind := *v["extension"]
		if kind == "" {
			kind = "terminal"
		}
		c, e := a.Launch(*v["view"], *v["name"], kind, *v["profile"], *v["folder"], nil)
		if e != nil {
			return e
		}
		a.RefreshOpen(*v["view"])
		return printJSON(c)
	case "restart", "stop", "kill":
		if len(args) != 2 {
			return errors.New(i18n.T("cli.usage", "shellstudio "+args[0]+" CONSOLE_ID"))
		}
		switch args[0] {
		case "restart":
			return a.Restart(args[1])
		case "kill":
			return a.Kill(args[1])
		}
		return a.Stop(args[1])
	case "extensions":
		if len(args) == 1 {
			return printJSON(extensions.Catalog(a.Paths.Config))
		}
		if len(args) == 2 && args[1] == "manage" {
			return bubbleTea(a, ui.New(a, "extensions"))
		}
		return errors.New(i18n.T("cli.usage", "shellstudio extensions [manage]"))
	case "permissions":
		return permissionsCommand(a, args[1:])
	case "notes":
		return bubbleTea(a, ui.New(a, "notes"))
	case "viewer":
		if len(args) < 2 || len(args) > 3 {
			return errors.New(i18n.T("cli.usage", "shellstudio viewer agent-chat|agent-gantt [DATABASE]"))
		}
		kind := args[1]
		if kind != "agent-chat" && kind != "agent-gantt" {
			return errors.New(i18n.T("cli.unknown_viewer"))
		}
		file := "messages.sqlite3"
		if kind == "agent-gantt" {
			file = "projects.sqlite3"
		}
		p := filepath.Join(a.Paths.Data, "extension-data", kind, file)
		if len(args) == 3 {
			if p, e = filepath.Abs(args[2]); e != nil {
				return e
			}
		}
		m := ui.New(a, "viewer")
		m.SetViewer(kind, p)
		return bubbleTea(a, m)
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
		diagnostics.Record(diagnostics.Entry{Event: "console-exec", Reference: c.ID}, nil, time.Time{})
		env := map[string]string{"SHELLSTUDIO_CONSOLE": c.ID}
		for k, v := range c.Env {
			env[k] = v
		}
		return syscall.Exec(p, c.Argv, runner.Env(env))
	}
	// Internal commands run from tmux bindings, popups and view panes.
	return internal(a, args)
}

func internal(a *app.App, args []string) error {
	report := func(context string, e error) error {
		if e != nil {
			a.RecordError(e, context)
			// Never display an asynchronous error on an unrelated client.
			client, pane, tty := "", "", ""
			for i := 1; i+1 < len(args); i++ {
				switch args[i] {
				case "--client":
					client = args[i+1]
				case "--pane", "--origin":
					pane = args[i+1]
				case "--tty":
					tty = args[i+1]
				}
			}
			if pane == "" && tty != "" {
				pane, _ = a.PaneByTTY(tty)
			}
			if client == "" && pane != "" {
				client, _ = a.ClientForPane(pane)
			}
			a.ShowError(client, context, e.Error())
		}
		return nil
	}
	switch args[0] {
	case "_message":
		if len(args) != 2 {
			return errors.New("usage: _message FILE")
		}
		b, e := os.ReadFile(args[1])
		if e != nil {
			return e
		}
		var message struct{ Title, Detail string }
		if e = json.Unmarshal(b, &message); e != nil {
			return e
		}
		s, e := tui.Open(false)
		if e != nil {
			return e
		}
		defer s.Close()
		tui.Message(s, message.Title, message.Detail)
		return nil
	case "_empty":
		return tui.Empty()
	case "_disconnected":
		return tui.Disconnected(a)
	case "_explorer":
		if len(args) != 2 {
			return errors.New("usage: _explorer VIEW")
		}
		return tui.Explorer(a, args[1])
	case "_minimized":
		if len(args) != 2 {
			return errors.New("usage: _minimized VIEW")
		}
		return tui.Minimized(a, args[1])
	case "_close":
		// Leaving nano closes its console. It runs inside the console that ends.
		if len(args) != 2 {
			return errors.New("usage: _close CONSOLE")
		}
		return report("Close editor", a.Kill(args[1]))
	case "_sync":
		if len(args) != 2 {
			return errors.New("usage: _sync VIEW")
		}
		return report("Reconnect", a.Sync(args[1]))
	case "_view":
		if len(args) < 2 {
			return errors.New("usage: _view ACTION")
		}
		v, sw, _, e := flags("_view", args[2:], "client", "pane", "layout", "origin", "target", "!back")
		if e != nil {
			return e
		}
		client, pane := *v["client"], *v["pane"]
		switch args[1] {
		case "next", "previous":
			delta := 1
			if args[1] == "previous" {
				delta = -1
			}
			return report("Change view", a.SwitchView(client, delta))
		case "layout":
			f, e := a.Focus(client, pane, false)
			if e != nil {
				return report("Layout", e)
			}
			return report("Layout", a.ApplyLayout(f.View, *v["layout"], f.Console))
		case "drop":
			return report("Drop path", a.Drop(*v["origin"], *v["target"]))
		case "tab":
			return report("Change view", a.SwitchToTab(client, *v["target"]))
		case "tagchoose":
			// Popup fallback of the native tag menu.
			s, e := tui.Open(false)
			if e != nil {
				return e
			}
			if err := tui.ChooseTag(s, a, *v["target"]); err != nil {
				tui.Message(s, "ACTION COULD NOT BE COMPLETED", err.Error())
			}
			s.Close()
			return nil
		case "restore-in":
			s, e := tui.Open(false)
			if e != nil {
				return e
			}
			view, err := tui.RestoreInDialog(s, a, client, *v["target"])
			if err != nil {
				a.RecordError(err, "Restore in another view")
				tui.Message(s, "ACTION COULD NOT BE COMPLETED", err.Error())
			}
			s.Close()
			if err == nil && view != "" {
				return report("Restore in another view", a.Open(view, client))
			}
			return nil
		case "rename-view", "close-view":
			return report("View tab", tui.RunTopBar(a, args[1], client, *v["target"]))
		case "toggle-explorer", "toggle-minimized":
			f, e := a.Focus(client, pane, true)
			if e != nil {
				return report("Panels", e)
			}
			if args[1] == "toggle-explorer" {
				return report("Panels", a.ToggleExplorer(f.View))
			}
			return report("Panels", a.ToggleMinimizedPanel(f.View))
		case "topmenu":
			return report("Menu", a.TopMenu(client, pane, *v["target"]))
		case "describe":
			// The console is the target (Minimized panel) or the focused one.
			console := *v["target"]
			if console == "" {
				f, e := a.Focus(client, pane, false)
				if e != nil {
					return report("Description", e)
				}
				console = f.Console
			}
			s, e := tui.Open(false)
			if e != nil {
				return e
			}
			err := tui.DescribeDialog(s, a, console)
			if err != nil {
				a.RecordError(err, "Description")
				tui.Message(s, "ACTION COULD NOT BE COMPLETED", err.Error())
			}
			s.Close()
			return nil
		case "move-before", "move-after":
			f, e := a.Focus(client, pane, false)
			if e != nil || f.Console == "" {
				return report("Move console", e)
			}
			delta := 1
			if args[1] == "move-before" {
				delta = -1
			}
			return report("Move console", a.Move(f.View, f.Console, delta))
		}
		return report("Dialog", tui.RunView(a, args[1], client, pane, *sw["back"]))
	case "_dialog":
		if len(args) < 2 {
			return errors.New("usage: _dialog ports|upload")
		}
		v, _, _, e := flags("_dialog", args[2:], "client", "pane", "source", "view", "target", "tty")
		if e != nil {
			return e
		}
		if args[1] == "upload" {
			return report("Upload from the PC", tui.Upload(a, *v["source"], *v["view"], *v["target"], *v["client"]))
		}
		client, pane := *v["client"], *v["pane"]
		if *v["tty"] != "" {
			pane, e = a.PaneByTTY(*v["tty"])
			if e != nil {
				return report("Dialog", e)
			}
		}
		if client == "" {
			client, e = a.ClientForPane(pane)
			if e != nil {
				return report("Dialog", e)
			}
		}
		return report("Dialog", a.OpenDialog(args[1], client, pane))
	case "_action":
		if len(args) < 2 {
			return errors.New("usage: _action ACTION")
		}
		v, _, _, e := flags("_action", args[2:], "pane", "client", "session", "tty", "target")
		if e != nil {
			return e
		}
		return report("Panel action", paneAction(a, args[1], *v["pane"], *v["client"], *v["session"], *v["tty"], *v["target"]))
	case "_resize_window":
		v, sw, _, e := flags("_resize_window", args[1:], "client", "worker", "!immediate")
		if e != nil {
			return nil
		}
		a.ResizeWindow(*v["client"], *v["worker"], *sw["immediate"])
		return nil
	}
	return errors.New(i18n.T("cli.unknown_command", args[0]))
}

// paneAction is what the header menus and finished consoles choose. It runs
// from run-shell -b: errors open a dialog in the initiating client.
func paneAction(a *app.App, action, pane, client, session, tty, target string) error {
	var e error
	if tty != "" {
		if pane, e = a.PaneByTTY(tty); e != nil {
			return e
		}
	}
	var f app.Focus
	switch {
	case client != "":
		if f, e = a.Focus(client, pane, false); e != nil {
			return e
		}
		pane = f.Pane
	case pane != "":
		if f, e = a.PaneOwner(pane); e != nil {
			return e
		}
	default:
		f.Console = strings.TrimPrefix(session, "c-")
	}
	// Actions on a console listed in a panel (Minimized) name it explicitly;
	// the pane then only identifies the view.
	if session != "" && (client != "" || pane != "") {
		f.Console = strings.TrimPrefix(session, "c-")
	}
	switch {
	case action == "minimize" && f.Console != "" && f.View != "":
		return a.Minimize(f.View, f.Console)
	case action == "restore" && f.Console != "" && f.View != "":
		return a.Restore(f.View, f.Console)
	case action == "minprefs":
		if e = tui.UpdateMinPrefs(a.Paths.Config, target); e == nil {
			a.NotifyMinimized()
		}
		return e
	case action == "panel-sizes" && pane != "":
		return a.SavePanelSizes(pane)
	case action == "maximize" && pane != "":
		return a.Maximize(pane)
	case action == "tagmenu" && f.Console != "":
		return a.TagMenu(client, pane, f.Console)
	case action == "tag" && f.Console != "":
		tag := target
		if tag == "none" {
			tag = ""
		}
		return a.SetTag(f.Console, tag)
	case action == "describe" && f.Console != "":
		return a.OpenDialogFor("describe", client, pane, f.Console)
	case action == "move":
		l, e := platform.LockWait(filepath.Join(a.Paths.Runtime, "render.lock"), 10*time.Second)
		if e != nil {
			return e
		}
		defer platform.Unlock(l)
		other, e := a.PaneOwner(target)
		if e != nil {
			return e
		}
		if other.View != f.View {
			return errors.New(i18n.T("cli.other_views"))
		}
		if f.Console != "" && other.Console != "" {
			if e = a.Store.Swap(f.View, f.Console, other.Console); e != nil {
				return e
			}
			_, e = a.Mux.Run(true, "swap-pane", "-d", "-s", pane, "-t", target)
			return e
		}
	case action == "rename":
		// The new name travels in a tmux option, never through a shell.
		var name string
		if pane != "" {
			o, _ := a.Mux.Run(true, "show-options", "-pqv", "-t", pane, "@ss_rename")
			a.Mux.Run(true, "set-option", "-p", "-qu", "-t", pane, "@ss_rename")
			name = strings.TrimSpace(o)
		} else {
			o, _ := a.Mux.Run(false, "show-options", "-qv", "-t", "="+session+":", "@ss_rename")
			a.Mux.Run(false, "set-option", "-qu", "-t", "="+session+":", "@ss_rename")
			name = strings.TrimSpace(o)
		}
		if name != "" && f.Console != "" {
			return a.Rename(f.Console, name)
		}
	case (action == "explorer" || action == "remove") && f.Kind == "explorer":
		return a.ToggleExplorer(f.View)
	case action == "remove" && f.Console != "" && f.View != "":
		return a.RemoveFromView(f.View, f.Console)
	case action == "kill" && f.Console != "":
		return a.Kill(f.Console)
	}
	return nil
}

// bubbleTea runs the Notes, Extensions and viewer screens. The background is
// declared dark so lipgloss never queries the terminal for its colours.
func bubbleTea(a *app.App, model *ui.Model) error {
	lipgloss.SetHasDarkBackground(true)
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
	_, e := p.Run()
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

// permissionsCommand lists or sets the preset new agent consoles start with.
func permissionsCommand(a *app.App, args []string) error {
	switch len(args) {
	case 0:
		type mode struct {
			ID, Label, Detail string
			Args              []string
			Selected          bool
		}
		out := map[string][]mode{}
		for _, agent := range app.PermissionAgents {
			cur, e := a.Permission(agent)
			if e != nil {
				return e
			}
			for _, p := range app.Permissions(agent) {
				out[agent] = append(out[agent], mode{p.ID, p.Label, p.Detail, p.Args, p.ID == cur.ID})
			}
		}
		return printJSON(out)
	case 2:
		p, e := app.FindPermission(args[0], args[1])
		if e != nil {
			return e
		}
		if e = a.SetPermission(args[0], p.ID); e != nil {
			return e
		}
		if p.Dangerous {
			fmt.Fprintln(os.Stderr, i18n.T("cli.perm_warning", app.AgentLabel(args[0])))
		}
		shown := i18n.T("cli.no_flags")
		if len(p.Args) > 0 {
			shown = strings.Join(p.Args, " ")
		}
		label, _ := app.PermissionText(args[0], p)
		fmt.Println(i18n.T("cli.perm_set", app.AgentLabel(args[0]), label, shown))
		return nil
	}
	return errors.New(i18n.T("cli.usage", "shellstudio permissions [claude|codex MODE]"))
}
