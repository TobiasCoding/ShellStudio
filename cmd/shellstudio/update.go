// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"shellstudio/internal/i18n"
	"shellstudio/internal/update"
)

// Checks happen before SQLite/tmux open. Offline startup stays usable.
func startupUpdate(manual, checkOnly bool) error {
	justUpdated := os.Getenv("SHELLSTUDIO_JUST_UPDATED") == "1"
	os.Unsetenv("SHELLSTUDIO_JUST_UPDATED")
	if !manual && (os.Getenv("SHELLSTUDIO_NO_UPDATE_CHECK") == "1" || justUpdated || !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd())) {
		return nil
	}
	client := update.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, e := client.Check(ctx)
	cancel()
	if e != nil {
		if manual {
			return e
		}
		fmt.Fprintln(os.Stderr, i18n.T("cli.update_off"))
		return nil
	}
	if !update.Newer(release.Tag, version) {
		if manual {
			fmt.Println(i18n.T("cli.up_to_date", version))
		}
		return nil
	}
	if checkOnly {
		fmt.Println(i18n.T("cli.available", strings.TrimPrefix(release.Tag, "v"), version))
		return nil
	}
	tty, e := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if e != nil {
		if manual {
			return fmt.Errorf("%s: %w", i18n.T("cli.update_tty"), e)
		}
		return nil
	}
	defer tty.Close()
	if !update.Prompt(tty, tty, version, release.Tag) {
		fmt.Fprintln(tty, i18n.T("cli.keeping"))
		return nil
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	fmt.Fprintln(tty, i18n.T("cli.downloading"))
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if e = client.Install(ctx, release, executable); e != nil {
		if manual {
			return e
		}
		fmt.Fprintln(tty, i18n.T("cli.update_failed", e))
		return nil
	}
	fmt.Fprintln(tty, i18n.T("cli.updated", strings.TrimPrefix(release.Tag, "v")))
	if manual {
		return nil
	}
	env := os.Environ()
	env = append(env, "SHELLSTUDIO_JUST_UPDATED=1")
	return syscall.Exec(executable, append([]string{executable}, os.Args[1:]...), env)
}
