// SPDX-License-Identifier: GPL-3.0-only
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Do not embed bytes.Buffer: its promoted ReadFrom would bypass our Write cap.
type limited struct{ buffer bytes.Buffer }

func (b *limited) String() string { return b.buffer.String() }

func (b *limited) Write(p []byte) (int, error) {
	n := len(p)
	if b.buffer.Len() < 65536 {
		left := 65536 - b.buffer.Len()
		if len(p) > left {
			p = p[:left]
		}
		b.buffer.Write(p)
	}
	return n, nil
}
func Env(extra map[string]string) []string {
	out := []string{}
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if k != "TMUX" && k != "TMUX_PANE" {
			if _, ok := extra[k]; !ok {
				out = append(out, v)
			}
		}
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}
func Run(timeout time.Duration, dir string, env map[string]string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Env = Env(env)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	c.WaitDelay = time.Second
	var b limited
	c.Stdout = &b
	c.Stderr = &b
	e := c.Run()
	if e != nil {
		if ctx.Err() != nil {
			return b.String(), fmt.Errorf("%s timed out after %s", name, timeout)
		}
		return b.String(), fmt.Errorf("%s: %w: %s", name, e, strings.TrimSpace(b.String()))
	}
	return b.String(), nil
}
