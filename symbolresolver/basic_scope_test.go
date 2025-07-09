package symbolresolver

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	templparser "github.com/a-h/templ/parser/v2"
)

// TestBasicScopes tests basic scope assignment using a simple test file
func TestBasicScopes(t *testing.T) {
	// Load the basic test file
	testFile, err := filepath.Abs("_testdata/basic.templ")
	if err != nil {
		t.Fatalf("failed to get absolute path: %v", err)
	}

	// Parse the template file
	tf, err := templparser.Parse(testFile)
	if err != nil {
		t.Fatalf("failed to parse template file: %v", err)
	}
	tf.Filepath = testFile

	// Create resolver and preprocess
	resolver := NewSymbolResolverV2()

	err = resolver.PreprocessFiles([]string{testFile})
	if err != nil {
		t.Fatalf("failed to preprocess files: %v", err)
	}

	// Assign scopes
	err = resolver.AssignScopes(tf)
	if err != nil {
		t.Logf("scope assignment error: %v", err)
	}

	// Test file scope
	t.Run("FileScope", func(t *testing.T) {
		if tf.Scope() == nil || tf.Scope().GoScope == nil {
			t.Fatal("template file should have scope")
		}

		fileScope := tf.Scope().GoScope
		t.Logf("File scope depth: %d", getScopeDepth(tf.Scope()))

		// Try to find what's actually in the scope by checking parent
		if parent := fileScope.Parent(); parent != nil {
			t.Logf("Parent scope type: %T", parent)
			t.Logf("Parent scope string: %s", parent)
		}

		// Check for global variable using LookupParent (searches up the scope chain)
		if _, obj := fileScope.LookupParent("globalVar", token.NoPos); obj != nil {
			t.Logf("✓ Found globalVar via LookupParent: %s", obj.Type())
		} else {
			t.Error("globalVar should be accessible from file scope")
		}

		// Check for helper function
		if _, obj := fileScope.LookupParent("helperFunc", token.NoPos); obj != nil {
			t.Logf("✓ Found helperFunc via LookupParent: %s", obj.Type())
		} else {
			t.Error("helperFunc should be accessible from file scope")
		}

		// Check for User type
		if _, obj := fileScope.LookupParent("User", token.NoPos); obj != nil {
			t.Logf("✓ Found User type via LookupParent: %s", obj.Type())
		} else {
			t.Error("User type should be accessible from file scope")
		}
	})

	// Test template scopes
	templates := make(map[string]*templparser.HTMLTemplate)
	for _, node := range tf.Nodes {
		if tmpl, ok := node.(*templparser.HTMLTemplate); ok {
			name := extractTemplateName(tmpl)
			templates[name] = tmpl
		}
	}

	t.Run("SimpleTemplate", func(t *testing.T) {
		tmpl, ok := templates["SimpleTemplate"]
		if !ok {
			t.Fatal("SimpleTemplate not found")
		}

		if tmpl.Scope() == nil || tmpl.Scope().GoScope == nil {
			t.Fatal("SimpleTemplate should have scope")
		}

		scope := tmpl.Scope().GoScope

		// Check for parameter
		if obj := scope.Lookup("name"); obj != nil {
			t.Logf("✓ Found parameter 'name' in SimpleTemplate scope: %s", obj.Type())
		} else {
			t.Error("parameter 'name' should be in SimpleTemplate scope")
		}
	})

	t.Run("WithMultipleParams", func(t *testing.T) {
		tmpl, ok := templates["WithMultipleParams"]
		if !ok {
			t.Fatal("WithMultipleParams not found")
		}

		if tmpl.Scope() == nil || tmpl.Scope().GoScope == nil {
			t.Fatal("WithMultipleParams should have scope")
		}

		scope := tmpl.Scope().GoScope

		// Check for parameters
		if obj := scope.Lookup("title"); obj != nil {
			t.Logf("✓ Found parameter 'title': %s", obj.Type())
		} else {
			t.Error("parameter 'title' should be in scope")
		}

		if obj := scope.Lookup("count"); obj != nil {
			t.Logf("✓ Found parameter 'count': %s", obj.Type())
		} else {
			t.Error("parameter 'count' should be in scope")
		}
	})

	// Test for loop scope
	t.Run("ForLoopScope", func(t *testing.T) {
		tmpl, ok := templates["WithForLoop"]
		if !ok {
			t.Fatal("WithForLoop not found")
		}

		// Find the for expression
		var forExpr *templparser.ForExpression
		walkTemplateNodes(tmpl.Children, func(n templparser.Node) bool {
			if fe, ok := n.(*templparser.ForExpression); ok {
				forExpr = fe
				return false
			}
			return true
		})

		if forExpr == nil {
			t.Fatal("ForExpression not found")
		}

		if forExpr.Scope() == nil || forExpr.Scope().GoScope == nil {
			t.Fatal("ForExpression should have scope")
		}

		forScope := forExpr.Scope().GoScope

		// Check for loop variables
		if obj := forScope.Lookup("i"); obj != nil {
			t.Logf("✓ Found loop variable 'i': %s", obj.Type())
		} else {
			t.Error("loop variable 'i' should be in for scope")
		}

		if obj := forScope.Lookup("item"); obj != nil {
			t.Logf("✓ Found loop variable 'item': %s", obj.Type())
		} else {
			t.Error("loop variable 'item' should be in for scope")
		}
	})

	// Test expression resolution in context
	t.Run("ExpressionResolution", func(t *testing.T) {
		tmpl, ok := templates["ExpressionTest"]
		if !ok {
			t.Fatal("ExpressionTest not found")
		}

		scope := tmpl.Scope().GoScope

		// Test resolving various expressions
		testCases := []struct {
			expr     string
			expected string
		}{
			{"helperFunc()", "string"},
			{"globalVar", "string"},
			{"user.Name", "string"},
			{"user.GetEmail()", "string"},
		}

		for _, tc := range testCases {
			t.Run(tc.expr, func(t *testing.T) {
				expr, err := parser.ParseExpr(tc.expr)
				if err != nil {
					t.Fatalf("failed to parse expression %s: %v", tc.expr, err)
				}

				exprType, err := ResolveExpression(expr, scope)
				if err != nil {
					t.Errorf("failed to resolve %s: %v", tc.expr, err)
					return
				}

				typeStr := exprType.String()
				if typeStr == tc.expected {
					t.Logf("✓ Resolved %s to %s", tc.expr, typeStr)
				} else {
					t.Errorf("expected %s to resolve to %s, got %s", tc.expr, tc.expected, typeStr)
				}
			})
		}
	})
}

// TestComponentElementResolution tests that component elements can be resolved
func TestComponentElementResolution(t *testing.T) {
	// Load the basic test file
	testFile, err := filepath.Abs("_testdata/basic.templ")
	if err != nil {
		t.Fatalf("failed to get absolute path: %v", err)
	}

	// Parse the template file
	tf, err := templparser.Parse(testFile)
	if err != nil {
		t.Fatalf("failed to parse template file: %v", err)
	}
	tf.Filepath = testFile

	// Create resolver and preprocess
	resolver := NewSymbolResolverV2()
	err = resolver.PreprocessFiles([]string{testFile})
	if err != nil {
		t.Fatalf("failed to preprocess files: %v", err)
	}

	// Assign scopes
	err = resolver.AssignScopes(tf)
	if err != nil {
		t.Logf("scope assignment error: %v", err)
	}

	// Find ParentComponent template
	var parentTmpl *templparser.HTMLTemplate
	for _, node := range tf.Nodes {
		if tmpl, ok := node.(*templparser.HTMLTemplate); ok {
			if extractTemplateName(tmpl) == "ParentComponent" {
				parentTmpl = tmpl
				break
			}
		}
	}

	if parentTmpl == nil {
		t.Fatal("ParentComponent not found")
	}

	// Walk through and find component elements
	componentElements := 0
	walkTemplateNodes(parentTmpl.Children, func(n templparser.Node) bool {
		if elem, ok := n.(*templparser.Element); ok && elem.IsComponent() {
			componentElements++
			t.Logf("Found component element: %s", elem.Name)

			if elem.Scope() == nil || elem.Scope().GoScope == nil {
				t.Errorf("Component element %s should have scope", elem.Name)
				return true
			}

			// Test resolving the component name
			expr, err := parser.ParseExpr(elem.Name)
			if err != nil {
				t.Errorf("failed to parse component name %s: %v", elem.Name, err)
				return true
			}

			compType, err := ResolveExpression(expr, elem.Scope().GoScope)
			if err != nil {
				t.Errorf("failed to resolve component %s: %v", elem.Name, err)
			} else {
				t.Logf("✓ Resolved component %s to type: %s", elem.Name, compType)
			}

			// Test resolving attributes
			for _, attr := range elem.Attributes {
				if ea, ok := attr.(*templparser.ExpressionAttribute); ok {
					attrExpr, err := parser.ParseExpr(ea.Expression.Value)
					if err == nil {
						attrType, err := ResolveExpression(attrExpr, elem.Scope().GoScope)
						if err == nil {
							t.Logf("  ✓ Resolved attribute %s = { %s } to type: %s",
								ea.Key.String(), ea.Expression.Value, attrType)
						} else {
							t.Logf("  ✗ Failed to resolve attribute %s = { %s }: %v",
								ea.Key.String(), ea.Expression.Value, err)
						}
					}
				}
			}
		}
		return true
	})

	if componentElements == 0 {
		t.Error("No component elements found in ParentComponent")
	}
}

