// SPDX-License-Identifier: GPL-3.0-only
package app

import (
	"bytes"
	"errors"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"shellstudio/internal/mux"
)

type Port struct {
	Number  int
	Program string
}

// ListeningPorts are TCP ports listening on this server and reachable on
// localhost, read from /proc/net without privileges. The owner is known only
// for this user's processes.
func ListeningPorts() []Port {
	found := map[int]string{}
	for _, src := range []struct {
		path string
		v6   bool
	}{{"/proc/net/tcp", false}, {"/proc/net/tcp6", true}} {
		b, e := os.ReadFile(src.path)
		if e != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		for _, line := range lines[1:] {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] != "0A" { // 0A: LISTEN
				continue
			}
			addr, hexPort, _ := strings.Cut(f[1], ":")
			n, e := strconv.ParseInt(hexPort, 16, 32)
			if e != nil {
				continue
			}
			port := int(n)
			reachable := false
			if src.v6 {
				reachable = addr == strings.Repeat("0", 32) || addr == strings.Repeat("0", 24)+"01000000"
			} else {
				reachable = addr == "00000000" || strings.HasSuffix(addr, "7F")
			}
			// 22 is this very connection; 53 on 127.0.0.53 is the system DNS.
			if reachable && port != 22 && !(port == 53 && addr != "00000000") {
				if _, ok := found[port]; !ok {
					found[port] = f[9]
				}
			}
		}
	}
	inodes := map[string]int{}
	for port, inode := range found {
		inodes[inode] = port
	}
	owners := map[int]string{}
	procs, _ := os.ReadDir("/proc")
	for _, p := range procs {
		if _, e := strconv.Atoi(p.Name()); e != nil {
			continue
		}
		fds, e := os.ReadDir("/proc/" + p.Name() + "/fd")
		if e != nil {
			continue
		}
		for _, fd := range fds {
			link, e := os.Readlink("/proc/" + p.Name() + "/fd/" + fd.Name())
			if e != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			port, ok := inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")]
			if !ok {
				continue
			}
			cmd, _ := os.ReadFile("/proc/" + p.Name() + "/cmdline")
			argv := bytes.Split(cmd, []byte{0})
			program := filepath.Base(string(argv[0]))
			// python3 script.py: the useful name is the script's.
			if strings.HasPrefix(program, "python") {
				for _, a := range argv[1:] {
					if len(a) > 0 && a[0] != '-' {
						program = filepath.Base(string(a))
						break
					}
				}
			}
			owners[port] = program
		}
	}
	var out []Port
	for port := range found {
		out = append(out, Port{port, owners[port]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// AnswersHTTP tells which ports answer HTTP: those open in the browser.
func AnswersHTTP(ports []Port) map[int]bool {
	out := map[int]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, p := range ports {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			for _, host := range []string{"127.0.0.1", "::1"} {
				c, e := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 300*time.Millisecond)
				if e != nil {
					continue
				}
				c.SetDeadline(time.Now().Add(300 * time.Millisecond))
				c.Write([]byte("HEAD / HTTP/1.0\r\nHost: localhost\r\n\r\n"))
				b := make([]byte, 5)
				n, _ := c.Read(b)
				c.Close()
				mu.Lock()
				out[port] = string(b[:n]) == "HTTP/"
				mu.Unlock()
				return
			}
		}(p.Number)
	}
	wg.Wait()
	return out
}

// SSHTarget is user@server as seen by the incoming SSH connection.
func (a *App) SSHTarget(client string) string {
	session := ""
	if client != "" {
		o, _ := a.Mux.Run(true, "list-clients", "-F", "#{client_name}\t#{session_name}")
		for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
			c, s, _ := strings.Cut(l, "\t")
			if c == client {
				session = s
			}
		}
	}
	args := []string{"show-environment", "-g", "SSH_CONNECTION"}
	if session != "" {
		args = []string{"show-environment", "-t", "=" + session, "SSH_CONNECTION"}
	}
	o, _ := a.Mux.Run(true, args...)
	_, value, _ := strings.Cut(strings.TrimSpace(o), "=")
	f := strings.Fields(value)
	if len(f) != 4 {
		f = strings.Fields(os.Getenv("SSH_CONNECTION"))
	}
	host, port := "THIS-SERVER", "22"
	if len(f) == 4 {
		host, port = f[2], f[3]
	}
	name := "user"
	if u, e := user.Current(); e == nil {
		name = u.Username
	}
	if port == "22" {
		return name + "@" + host
	}
	return name + "@" + host + " -p " + port
}

// CopyToClient sends text to the PC clipboard through the client (OSC 52).
// tmux does not send an empty buffer: callers use a comment instead.
func (a *App) CopyToClient(client, text string) error {
	args := []string{"set-buffer", "-w"}
	if client != "" {
		args = append(args, "-t", client)
	}
	_, e := a.Mux.Run(true, append(args, "--", text)...)
	return e
}

var windowsDrive = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
var windowsShare = regexp.MustCompile(`^\\\\[^\\]+\\[^\\]+(\\.*)?$`)

// WindowsDropPath accepts one absolute path pasted by Windows Terminal on drop.
func WindowsDropPath(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	if strings.ContainsAny(value, "\r\n\"") || len(value) > 4096 {
		return ""
	}
	if windowsDrive.MatchString(value) || windowsShare.MatchString(value) {
		return value
	}
	return ""
}

// SCPCommand: PowerShell checks on the PC whether the path is a folder before
// choosing -r.
func (a *App) SCPCommand(source, root, directory, client string) (string, error) {
	source = WindowsDropPath(source)
	if source == "" {
		return "", errors.New("drag a single file or folder with an absolute Windows path")
	}
	target, e := Resolve(root, directory)
	if e != nil {
		return "", e
	}
	if fi, e := os.Stat(target); e != nil || !fi.IsDir() {
		return "", errors.New("the remote folder no longer exists or is outside the view folder")
	}
	address := a.SSHTarget(client)
	host, port, hasPort := strings.Cut(address, " -p ")
	if strings.Contains(host, "THIS-SERVER") {
		return "", errors.New("the SSH server could not be identified; reconnect the view through SSH")
	}
	u, hostname, _ := strings.Cut(host, "@")
	if strings.Contains(hostname, ":") && !strings.HasPrefix(hostname, "[") {
		host = u + "@[" + hostname + "]"
	}
	endpoint := host + ":" + strings.TrimRight(filepath.ToSlash(target), "/") + "/"
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	portArg := ""
	if hasPort {
		portArg = " -P " + port
	}
	return "$src = " + quote(source) + "; " +
		`if (-not (Test-Path -LiteralPath $src)) { throw "The local path does not exist" }; ` +
		`$opts = @(); if ((Get-Item -LiteralPath $src).PSIsContainer) { $opts += "-r" }; ` +
		"& scp.exe @opts" + portArg + " -- $src " + quote(endpoint), nil
}

// OpenUploadDialog shows the SCP command in a popup, without blocking the tree.
func (a *App) OpenUploadDialog(source, view, directory, client string) error {
	args := []string{"_dialog", "upload", "--source", source, "--view", view, "--target", directory, "--client", client}
	cmd := []string{a.Mux.Tmux, "-S", a.Mux.Socket(true), "display-popup", "-E", "-c", client, "-w", "85%", "-h", "14", a.Mux.Self(args...)}
	return startDetached(cmd)
}

// ExplorerClient is the client that last clicked the tree, or any client of the view.
func (a *App) ExplorerClient(p, view string) string {
	o, e := a.Mux.Run(true, "show-options", "-pqv", "-t", p, "@ss_client")
	if e == nil && strings.TrimSpace(o) != "" {
		return strings.TrimSpace(o)
	}
	o, _ = a.Mux.Run(true, "list-clients", "-F", "#{client_name}\t#{session_name}")
	for _, l := range strings.Split(strings.TrimSpace(o), "\n") {
		c, s, _ := strings.Cut(l, "\t")
		if s == mux.ViewSession(view) {
			return c
		}
	}
	return ""
}
