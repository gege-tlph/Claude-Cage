package uilocale

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// Every Chinese text the backend can show needs an English entry in
// ui/src/locales/en.json, whose keys the interface matches whole or as
// templates ({0}, {1} for format verbs and concatenated values). Text that is
// never shown as it is — shell scripts, page sources, terminal output — is
// listed in testdata/backend-text-without-english.json.
func TestBackendTextHasEnglish(t *testing.T) {
	root := filepath.Join("..", "..")
	var english map[string]string
	readJSON(t, filepath.Join(root, "ui", "src", "locales", "en.json"), &english)
	var exempt []string
	readJSON(t, filepath.Join("testdata", "backend-text-without-english.json"), &exempt)
	allowed := map[string]bool{}
	for _, text := range exempt {
		allowed[text] = true
	}

	texts := backendTexts(t, root)
	if len(texts) < 2000 {
		t.Fatalf("found only %d backend texts; the scan no longer reaches the sources", len(texts))
	}
	found := map[string]string{}
	for text, at := range texts {
		if english[text] == "" && !allowed[text] {
			found[text] = at
		}
	}
	missing := make([]string, 0, len(found))
	for text := range found {
		missing = append(missing, text)
	}
	sort.Strings(missing)
	for _, text := range missing {
		t.Errorf("%s: no English in ui/src/locales/en.json for %q", found[text], text)
	}
}

func readJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

var formatVerb = regexp.MustCompile(`%[-+# 0]*(\d+|\*)?(\.(\d+|\*))?[a-zA-Z%]`)

// Calls whose Chinese arguments are matched against output or logged, not shown.
var notShown = []string{"diagnostics.Log", "log.", "regexp.", "mux.HandleFunc", "strings.", "bytes."}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func templateOf(s string) string {
	n := 0
	return formatVerb.ReplaceAllStringFunc(s, func(v string) string {
		if v == "%%" {
			return "%"
		}
		n++
		return fmt.Sprintf("{%d}", n-1)
	})
}

func calleeName(e ast.Expr) string {
	switch f := e.(type) {
	case *ast.SelectorExpr:
		if x, ok := f.X.(*ast.Ident); ok {
			return x.Name + "." + f.Sel.Name
		}
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}
	return ""
}

func flatten(e ast.Expr, parts *[]string) {
	switch x := e.(type) {
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			flatten(x.X, parts)
			flatten(x.Y, parts)
			return
		}
	case *ast.ParenExpr:
		flatten(x.X, parts)
		return
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			if s, err := strconv.Unquote(x.Value); err == nil {
				*parts = append(*parts, s)
				return
			}
		}
	}
	*parts = append(*parts, "\x00")
}

// backendTexts maps each shown Chinese template to the first place it occurs.
func backendTexts(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == ".":
			case strings.HasPrefix(rel, "third_party"), strings.HasPrefix(rel, "ui"), strings.HasPrefix(rel, ".git"),
				strings.HasPrefix(rel, "tests"), strings.HasPrefix(rel, "www"), strings.HasPrefix(rel, "tools"),
				d.Name() == "testdata", d.Name() == "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		add := func(text string, pos token.Pos, call string) {
			for _, prefix := range notShown {
				if strings.HasPrefix(call, prefix) {
					return
				}
			}
			if _, ok := out[text]; !ok {
				out[text] = fmt.Sprintf("%s:%d", rel, fset.Position(pos).Line)
			}
		}
		done := map[ast.Node]bool{}
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			if done[n] {
				return true
			}
			call := ""
			for i := len(stack) - 1; i >= 0; i-- {
				if c, ok := stack[i].(*ast.CallExpr); ok {
					call = calleeName(c.Fun)
					break
				}
			}
			switch x := n.(type) {
			case *ast.BinaryExpr:
				if x.Op != token.ADD {
					if x.Op == token.EQL || x.Op == token.NEQ {
						ast.Inspect(x, func(m ast.Node) bool { done[m] = true; return true })
					}
					return true
				}
				var parts []string
				flatten(x, &parts)
				var b strings.Builder
				slot := 0
				for _, p := range parts {
					if p == "\x00" {
						fmt.Fprintf(&b, "{%d}", slot)
						slot++
						continue
					}
					b.WriteString(p)
				}
				if !hasHan(b.String()) {
					return true
				}
				ast.Inspect(x, func(m ast.Node) bool { done[m] = true; return true })
				text := b.String()
				if strings.HasPrefix(call, "fmt.") && strings.Contains(call, "f") {
					text = templateOf(text)
				}
				add(text, x.Pos(), call)
			case *ast.CaseClause:
				for _, e := range x.List {
					ast.Inspect(e, func(m ast.Node) bool { done[m] = true; return true })
				}
			case *ast.BasicLit:
				if x.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(x.Value)
				if err != nil || !hasHan(s) {
					return true
				}
				if strings.Contains(s, "%") && (strings.HasSuffix(call, "f") || call == "") {
					s = templateOf(s)
				}
				add(s, x.Pos(), call)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
