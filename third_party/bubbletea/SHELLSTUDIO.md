# Bubble Tea local patch

Source: https://github.com/charmbracelet/bubbletea/tree/v1.3.4
Module: github.com/charmbracelet/bubbletea v1.3.4
License: MIT, retained in LICENSE.

This directory contains the upstream Go source and module files, excluding tests.
The only source change is in tea_init.go: replace the automatic terminal
background query with lipgloss.SetHasDarkBackground(true). ShellStudio uses a
fixed dark theme. Upstream's init runs even for commands that never open a Bubble
Tea screen; the query could hang startup or leak late replies into tmux consoles.

The root module uses a local replace directive, so direct Go builds, tests and
release builds all get the same behavior. tests/terminal_integration.py verifies
that a CLI command emits no terminal control probes under a real PTY, and checks
workspace input while answering tmux's own terminal queries.
