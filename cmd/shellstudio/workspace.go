// SPDX-License-Identifier: GPL-3.0-only
package main

import "strings"

// Explicit paths (./notes or open notes) can always disambiguate a folder.
func reservedCommand(name string) bool {
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "_") {
		return true
	}
	switch name {
	case "update", "version", "help", "schema", "validate", "doctor", "report", "backup", "views", "consoles", "new-view", "launch", "restart", "stop", "kill", "extensions", "notes", "viewer":
		return true
	}
	return false
}
