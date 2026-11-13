// SPDX-License-Identifier: GPL-3.0-only
package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEveryKeyHasBothLanguages(t *testing.T) {
	for k, e := range catalog {
		if strings.TrimSpace(e.es) == "" || strings.TrimSpace(e.en) == "" {
			t.Errorf("%s lacks a translation: %+v", k, e)
		}
		if strings.Count(e.es, "%") != strings.Count(e.en, "%") {
			t.Errorf("%s: format verbs differ: %q / %q", k, e.es, e.en)
		}
	}
}

func TestLanguageSelection(t *testing.T) {
	defer SetLang(Lang())
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "es_AR.UTF-8")
	if Resolve("auto") != "es" || Resolve("") != "es" || Resolve("en") != "en" {
		t.Fatal("es locale")
	}
	t.Setenv("LC_ALL", "C")
	if Resolve("auto") != "en" || Resolve("es") != "es" {
		t.Fatal("LC_ALL wins")
	}
	SetLang("es")
	if T("top.views") != "VISTAS" || T("tags.delete_confirm", "X", 2) != "¿Borrar la etiqueta X? 2 consolas quedarán sin etiqueta." {
		t.Fatal(T("top.views"))
	}
	SetLang("fr")
	if Lang() != "en" || T("top.views") != "VIEWS" {
		t.Fatal(Lang())
	}
}

func TestMissingKeyPanicsUnderTest(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("missing key did not panic")
		}
	}()
	T("no.such.key")
}

// Text in tmux formats and menus must not break them in any language: #
// starts a format, a comma or brace ends a conditional branch, ; separates
// commands, quotes break the quoting, and a leading - disables a menu item.
func TestTmuxTextIsSafe(t *testing.T) {
	for k, e := range catalog {
		if !strings.HasPrefix(k, "tmux.") {
			continue
		}
		for _, v := range []string{e.es, e.en} {
			if strings.ContainsAny(v, "#,{};'\"") || strings.HasPrefix(v, "-") {
				t.Errorf("%s: %q is not safe inside a tmux format", k, v)
			}
		}
	}
}

// Every key written literally in the code exists: T only panics on the paths
// the tests run. Keys built from a prefix ("layout." + l) are left to the
// tests of their packages.
func TestEveryKeyInTheCodeExists(t *testing.T) {
	re := regexp.MustCompile(`i18n\.T\("([^"]+)"\s*[,)]`)
	found := 0
	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, m := range re.FindAllSubmatch(b, -1) {
				found++
				if !Has(string(m[1])) {
					t.Errorf("%s: missing key %s", p, m[1])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if found < 300 {
		t.Fatalf("only %d keys found: wrong root?", found)
	}
}

func TestErrorContextsTranslateForDisplay(t *testing.T) {
	defer SetLang(Lang())
	SetLang("es")
	if Context("Explorer shortcut") != "Atajo del explorador" || Context("something new") != "something new" {
		t.Fatal(Context("Explorer shortcut"))
	}
	SetLang("en")
	if Context("Explorer shortcut") != "Explorer shortcut" {
		t.Fatal("English keeps the recorded name")
	}
}

// Every literal context passed to RecordError or main's report() has a
// display translation.
func TestEveryErrorContextIsTranslated(t *testing.T) {
	re := regexp.MustCompile(`(?:RecordError\([^,\n]+, |report\()"([^"]+)"`)
	found := 0
	for _, root := range []string{"../../internal", "../../cmd"} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, _ := os.ReadFile(p)
			for _, m := range re.FindAllSubmatch(b, -1) {
				name := string(m[1])
				if name == "configuration" || name == "console server" || name == "tmux" || name == "database" {
					continue // doctor's report labels, not recorded errors
				}
				found++
				if !Has("ctx." + name) {
					t.Errorf("%s: context %q has no ctx. translation", p, name)
				}
			}
			return nil
		})
	}
	if found < 20 {
		t.Fatalf("only %d contexts found", found)
	}
}
