// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shellstudio/internal/diagnostics"
	"shellstudio/internal/platform"
)

func diagnosticReport(args []string) error {
	if len(args) > 1 || (len(args) == 1 && strings.HasPrefix(args[0], "-") && args[0] != "--stdout") {
		return errors.New("usage: shellstudio report [FILE | --stdout]")
	}
	report := diagnostics.Collect(version)
	if len(args) == 1 && args[0] == "--stdout" {
		return printJSON(report)
	}
	var path string
	if len(args) == 1 {
		path = args[0]
	} else {
		p, err := platform.Resolve()
		if err != nil {
			return fmt.Errorf("cannot locate report directory; use report --stdout: %w", err)
		}
		dir := filepath.Join(p.State, "reports")
		if err = platform.PrivateDir(dir); err != nil {
			return fmt.Errorf("cannot create report directory; use report --stdout: %w", err)
		}
		path = filepath.Join(dir, "shellstudio-report-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	fmt.Println("Diagnostic report:", path)
	return nil
}
