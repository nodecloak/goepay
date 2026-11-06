package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"goepay/internal/identity"
)

// routeFamiliesByHandler reads this package's console handlers and returns, per
// handler, every route family (the path segment after /console/v1/<role>/) the
// handler compares its route segments with. Handlers are recognised by their
// identity.Session and []string parameters; console() itself contributes the
// families it dispatches through its action variable.
func routeFamiliesByHandler(t *testing.T) map[string]map[string]bool {
	t.Helper()
	files, e := filepath.Glob("*.go")
	if e != nil {
		t.Fatal(e)
	}
	found := map[string]map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "admin_permissions.go" || name == "admin_menu_permissions.go" {
			continue
		}
		src, e := os.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		file, e := parser.ParseFile(fset, name, src, 0)
		if e != nil {
			t.Fatal(e)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			isConsole := fn.Name.Name == "console" && fn.Recv != nil
			route := "parts"
			if !isConsole {
				if !takesSession(fn) {
					continue
				}
				if route = routeParameter(fn); route == "" {
					continue
				}
			}
			family := func(e ast.Expr) bool {
				if index, ok := e.(*ast.IndexExpr); ok {
					if id, ok := index.X.(*ast.Ident); ok && id.Name == route {
						if lit, ok := index.Index.(*ast.BasicLit); ok && lit.Value == "1" {
							return true
						}
					}
				}
				id, ok := e.(*ast.Ident)
				return ok && isConsole && id.Name == "action"
			}
			record := func(e ast.Expr) {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil && v != "" {
						if found[fn.Name.Name] == nil {
							found[fn.Name.Name] = map[string]bool{}
						}
						found[fn.Name.Name][v] = true
					}
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BinaryExpr:
					if x.Op == token.EQL || x.Op == token.NEQ {
						if family(x.X) {
							record(x.Y)
						} else if family(x.Y) {
							record(x.X)
						}
					}
				case *ast.SwitchStmt:
					if x.Tag != nil && family(x.Tag) {
						for _, stmt := range x.Body.List {
							for _, value := range stmt.(*ast.CaseClause).List {
								record(value)
							}
						}
					}
				}
				return true
			})
		}
	}
	return found
}

// consoleRouteFamilies maps every console route family to one handler serving it.
func consoleRouteFamilies(t *testing.T) map[string]string {
	t.Helper()
	all := map[string]string{}
	for handler, families := range routeFamiliesByHandler(t) {
		for family := range families {
			all[family] = handler
		}
	}
	return all
}

// routeParameter names the handler's []string parameter: the console path
// segments, whatever the handler calls them.
func routeParameter(fn *ast.FuncDecl) string {
	for _, field := range fn.Type.Params.List {
		if array, ok := field.Type.(*ast.ArrayType); ok && array.Len == nil {
			if id, ok := array.Elt.(*ast.Ident); ok && id.Name == "string" && len(field.Names) == 1 {
				return field.Names[0].Name
			}
		}
	}
	return ""
}

func takesSession(fn *ast.FuncDecl) bool {
	for _, field := range fn.Type.Params.List {
		if sel, ok := field.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Session" {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "identity" {
				return true
			}
		}
	}
	return false
}

// Every family a console handler serves must be classified for operators. A
// new family therefore stays super-only until it is added to one of the lists.
func TestEveryConsoleRouteFamilyIsClassifiedForOperators(t *testing.T) {
	var unclassified []string
	for family, where := range consoleRouteFamilies(t) {
		if operatorRouteFamilies[family] == superOnlyRouteFamilies[family] {
			unclassified = append(unclassified, family+" ("+where+")")
		}
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Fatalf("classify these console route families in operatorRouteFamilies or superOnlyRouteFamilies:\n%s", strings.Join(unclassified, "\n"))
	}
	for family := range operatorRouteFamilies {
		if superOnlyRouteFamilies[family] {
			t.Fatalf("%s is listed as both operator and super-only", family)
		}
	}
}

func TestOperatorsAreDeniedUnlistedRouteFamilies(t *testing.T) {
	operator := identity.User{AdminClass: "operator"}
	for _, tc := range []struct {
		parts []string
		want  bool
	}{
		{[]string{"admin", "orders"}, true},
		{[]string{"admin", "orders", "202611060000000000000000001"}, true},
		{[]string{"admin", "session"}, true},
		{[]string{"admin", "resources", "merchants"}, true},
		{[]string{"admin", "resources", "settings"}, false},
		{[]string{"admin", "operations", "jobs"}, false},
		{[]string{"admin", "migration"}, false},
		{[]string{"admin", "migration", "epay", "prepare"}, false},
		{[]string{"admin", "a-family-added-later"}, false},
		{[]string{"admin", "a-family-added-later", "item"}, false},
	} {
		if got := adminRequestAllowed(operator, tc.parts); got != tc.want {
			t.Errorf("operator %v: got %v, want %v", tc.parts, got, tc.want)
		}
	}
	super := identity.User{AdminClass: "super"}
	if !adminRequestAllowed(super, []string{"admin", "a-family-added-later"}) {
		t.Fatal("super administrators are not limited by the operator list")
	}
}

// The route tables must list exactly the families each handler compares, name
// the method they call, and include every handler that guards a family.
func TestConsoleRoutesMatchHandlerFamilies(t *testing.T) {
	byHandler := routeFamiliesByHandler(t)
	routed := map[string]bool{}
	for _, table := range [][]consoleRoute{consoleRoutesBeforeFunds, consoleRoutesAfterFunds} {
		for _, route := range table {
			if routed[route.name] {
				t.Errorf("%s is routed twice", route.name)
			}
			routed[route.name] = true
			want := byHandler[route.name]
			got := map[string]bool{}
			for _, family := range route.families {
				got[family] = true
			}
			if len(want) == 0 || len(got) != len(want) {
				t.Errorf("%s declares %v, handler compares %v", route.name, route.families, keys(want))
				continue
			}
			for family := range want {
				if !got[family] {
					t.Errorf("%s does not declare family %q that it serves", route.name, family)
				}
			}
		}
	}
	for handler := range byHandler {
		if handler != "console" && !routed[handler] {
			t.Errorf("handler %s guards route families but is not in a console route table", handler)
		}
	}
	// Each entry's name must be the method its serve function calls.
	fset := token.NewFileSet()
	file, e := parser.ParseFile(fset, "console_routes.go", nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		literal, ok := n.(*ast.CompositeLit)
		if !ok || len(literal.Elts) != 3 {
			return true
		}
		name, ok := literal.Elts[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		want, _ := strconv.Unquote(name.Value)
		calls := map[string]bool{}
		ast.Inspect(literal.Elts[2], func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				calls[sel.Sel.Name] = true
			}
			return true
		})
		if !calls[want] {
			t.Errorf("route %q does not call the method it names", want)
		}
		return false
	})
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
