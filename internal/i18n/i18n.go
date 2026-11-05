// SPDX-License-Identifier: GPL-3.0-only

// Package i18n translates user-visible text. Logs and diagnostics stay in
// English. The language comes from config.json (language: auto|es|en), then
// LC_ALL, LC_MESSAGES or LANG starting with "es", then English.
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// Languages lists the supported languages in menu order.
var Languages = []string{"es", "en"}

var lang atomic.Value

func init() { lang.Store(FromEnv()) }

// FromEnv is the language the locale variables ask for.
func FromEnv() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(v), "es") {
			return "es"
		}
		return "en"
	}
	return "en"
}

// Resolve turns a configured value (auto, es, en or empty) into a language.
func Resolve(configured string) string {
	switch configured {
	case "es", "en":
		return configured
	}
	return FromEnv()
}

// SetLang selects the language; unknown values select English.
func SetLang(l string) {
	if l != "es" {
		l = "en"
	}
	lang.Store(l)
}

// Lang is the selected language.
func Lang() string { return lang.Load().(string) }

// T translates a key, formatting args with fmt.Sprintf when given. A missing
// key panics under go test and otherwise shows the key itself.
func T(key string, args ...any) string {
	e, ok := catalog[key]
	if !ok {
		if testing.Testing() {
			panic("i18n: missing key " + key)
		}
		return key
	}
	s := e.en
	if Lang() == "es" {
		s = e.es
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// Has reports whether a key exists.
func Has(key string) bool { _, ok := catalog[key]; return ok }

type entry struct{ es, en string }
