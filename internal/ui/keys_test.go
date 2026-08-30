// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/trm42/smartview/internal/config"
	"github.com/trm42/smartview/internal/smart"
)

// The '?' modal promises to list every binding, and that promise has drifted
// before: 'x' (the only way to abort a multi-hour self-test), '?' itself and the
// paging keys were all bound and undocumented. These tests hold the promise by
// deriving the bound keys from the package's own source rather than from a hand
// written list, so adding a binding without documenting it fails the build.

// documentedKeys returns the key tokens named in the left column of keysText —
// "Tab", "j", "PgUp", "1-9" and so on. Only the column is used, never the whole
// line: a description like "page content" contains the letters of half the
// alphabet, and matching against those would pass for keys nobody documented.
func documentedKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, line := range strings.Split(keysText, "\n") {
		// The column is separated from the description by two or more spaces.
		label, _, found := strings.Cut(line, "  ")
		if !found || strings.TrimSpace(label) == "" {
			continue // heading or blank line
		}
		// Labels combine keys with "/" ("g / G", "PgUp/PgDn", "↑/↓") — split on
		// both the slash and whitespace so each key stands alone. "1-9" is a
		// range and is deliberately left whole.
		for _, tok := range strings.FieldsFunc(label, func(r rune) bool {
			return r == '/' || unicode.IsSpace(r)
		}) {
			keys[tok] = true
		}
	}
	if len(keys) < 10 {
		t.Fatalf("parsed only %d key tokens from keysText — the column format changed: %v", len(keys), keys)
	}
	return keys
}

// boundRunes parses every non-test source file of this package and collects the
// rune literals the key handlers actually match: the case values of a switch on
// ev.Rune() (App.onKey, onFleetKey, the attribute table, scrollView) and the
// literal side of a direct `ev.Rune() == 'x'` comparison (the Tests tab).
// It returns each rune with the position that bound it, for a useful failure.
func boundRunes(t *testing.T) map[rune]string {
	t.Helper()
	fset := token.NewFileSet()
	// parser.ParseDir is deprecated (it ignores build tags), so walk the
	// directory and parse each non-test source file of this package instead.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if f.Name.Name != "ui" {
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("no package ui source files found")
	}

	runes := map[rune]string{}
	record := func(lit *ast.BasicLit) {
		if lit.Kind != token.CHAR {
			return
		}
		r, _, _, err := strconv.UnquoteChar(strings.Trim(lit.Value, "'"), '\'')
		if err != nil {
			t.Fatalf("%s: cannot decode rune literal %s: %v", fset.Position(lit.Pos()), lit.Value, err)
		}
		if _, seen := runes[r]; !seen {
			runes[r] = fset.Position(lit.Pos()).String()
		}
	}

	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.SwitchStmt:
				// `switch ev.Rune() {`, `switch r := ev.Rune(); r {` and the
				// tagless `switch r := ev.Rune(); { case r == 't': }` all count:
				// each dispatches on a key, so every rune in its cases is bound.
				if !mentionsRuneCall(node.Init) && !mentionsRuneCall(node.Tag) {
					return true
				}
				for _, stmt := range node.Body.List {
					clause, ok := stmt.(*ast.CaseClause)
					if !ok {
						continue
					}
					for _, expr := range clause.List {
						ast.Inspect(expr, func(n ast.Node) bool {
							if lit, ok := n.(*ast.BasicLit); ok {
								record(lit)
							}
							return true
						})
					}
				}
			case *ast.BinaryExpr:
				// A bare comparison, e.g. `ev.Rune() == 'x'` in the Tests view.
				if node.Op != token.EQL {
					return true
				}
				if mentionsRuneCall(node.X) {
					if lit, ok := node.Y.(*ast.BasicLit); ok {
						record(lit)
					}
				}
				if mentionsRuneCall(node.Y) {
					if lit, ok := node.X.(*ast.BasicLit); ok {
						record(lit)
					}
				}
			}
			return true
		})
	}
	if len(runes) < 10 {
		t.Fatalf("found only %d bound runes — the scan missed the key handlers: %v", len(runes), runes)
	}
	return runes
}

// mentionsRuneCall reports whether an expression (or statement) contains a call
// to a .Rune() method, which is how tcell key events yield their character.
func mentionsRuneCall(n ast.Node) bool {
	if n == nil {
		return false
	}
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Rune" {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestKeysModalDocumentsEveryBoundRune is the guard the '?' modal's promise
// needs: every rune the package binds must appear in the modal's key column.
func TestKeysModalDocumentsEveryBoundRune(t *testing.T) {
	documented := documentedKeys(t)
	for r, pos := range boundRunes(t) {
		// The nine tab/section digits are documented as the range "1-9".
		if unicode.IsDigit(r) {
			if !documented["1-9"] && !documented[string(r)] {
				t.Errorf("digit %q bound at %s is not documented (expected the range \"1-9\")", r, pos)
			}
			continue
		}
		if !documented[string(r)] {
			t.Errorf("key %q bound at %s is missing from the '?' modal (keysText)", r, pos)
		}
	}
}

// TestKeysModalDocumentsNamedKeys covers the bindings that are not runes, so the
// AST scan cannot see them: they are matched as tcell.Key constants. This list
// is hand kept — add to it when a handler matches a new tcell.Key.
func TestKeysModalDocumentsNamedKeys(t *testing.T) {
	documented := documentedKeys(t)
	for _, key := range []string{
		"Tab",   // toggleFocus
		"Esc",   // back / quit
		"Enter", // start a self-test, open a drive from the fleet
		"↑",     // select drive (narrow layout steps the selection itself)
		"↓",
		"←", // pane / tab navigation
		"→",
		"PgUp", // scrollView paging
		"PgDn",
		"^B", // the Ctrl aliases scrollView binds beside PgUp/PgDn
		"^F",
		"Home", // scrollView jump to top / bottom
		"End",
	} {
		if !documented[key] {
			t.Errorf("%s is bound but missing from the '?' modal (keysText)", key)
		}
	}
}

// TestKeysModalSpecificBindings pins the three bindings that were missing when
// the modal claimed to be complete, so a future edit cannot drop them again.
func TestKeysModalSpecificBindings(t *testing.T) {
	for _, want := range []string{
		"x", // cancel a running self-test — the only way to abort a long one
		"?", // the modal that lists the keys must list itself
		"g", // paging / jump keys
		"G",
		"j",
		"k",
		"R", // the only way to read a drive standby_aware is skipping
	} {
		if !documentedKeys(t)[want] {
			t.Errorf("%q missing from the '?' modal", want)
		}
	}
}

// modalWrapWidth is what tview.Modal allows its text at the narrowest terminal
// smartview supports: it word-wraps at a third of the screen width, so at 80
// columns a line over 26 cells is split and the two-column layout collapses.
const modalWrapWidth = 80 / 3

func TestKeysModalFitsTheNarrowestTerminal(t *testing.T) {
	for _, line := range strings.Split(keysText, "\n") {
		if n := len([]rune(line)); n > modalWrapWidth {
			t.Errorf("keys line %q is %d cells, over the %d that fit at 80 columns", line, n, modalWrapWidth)
		}
	}
}

// TestContextHintsFollowTheLiveAttributesView pins a hint bar that advertised
// keys nothing listened for. contextHints keys off the tab id, which is
// "attributes" for both protocols, but only the ATA table binds s/f — the NVMe
// health view installs no input capture, so those keys fell through to onKey
// and were dropped. The hint now follows the live view, not the id.
func TestContextHintsFollowTheLiveAttributesView(t *testing.T) {
	cases := []struct {
		name      string
		report    *smart.Report
		wantHints bool
	}{
		{"ata", &smart.Report{
			Device:        smart.Device{Protocol: "ATA"},
			ATAAttributes: &smart.ATAAttributes{Table: []smart.ATAAttribute{{ID: 5, Name: "Reallocated_Sector_Ct"}}},
		}, true},
		{"nvme", &smart.Report{
			Device:     smart.Device{Protocol: "NVMe"},
			NVMeHealth: &smart.NVMeHealth{},
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := New(config.Default(), func(config.Config) error { return nil })
			// New installs the theme globally; put it back so test order cannot matter.
			t.Cleanup(func() { setTheme(themes["dark"]) })
			c.report.Device.Name = "/dev/sdz"
			a.detail.update(c.report, nil)
			if !a.detail.selectTabID("attributes") {
				t.Fatal("no attributes tab for this report")
			}
			// The context hints only apply while the detail holds focus.
			a.app.SetFocus(a.detail.content())

			got := stripTags(a.contextHints())
			switch {
			case c.wantHints && !strings.Contains(got, "sort"):
				t.Errorf("ATA attributes hints = %q, want the s/f keys it binds", got)
			case !c.wantHints && got != "":
				t.Errorf("NVMe attributes hints = %q, want none (it binds no keys)", got)
			}
		})
	}
}
