package symbolresolver

import (
	"go/parser"
	"go/token"
	"go/types"
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
			if obj.Type().String() != "string" {
				t.Errorf("globalVar should be string, got %s", obj.Type())
			}
		} else {
			t.Error("globalVar should be accessible from file scope")
		}

		// Check for helper function
		if _, obj := fileScope.LookupParent("helperFunc", token.NoPos); obj != nil {
			if obj.Type().String() != "func() string" {
				t.Errorf("helperFunc should be func() string, got %s", obj.Type())
			}
		} else {
			t.Error("helperFunc should be accessible from file scope")
		}

		// Check for User type
		if _, obj := fileScope.LookupParent("User", token.NoPos); obj != nil {
			// For type definitions, check if it's a named type
			if named, ok := obj.Type().(*types.Named); ok {
				if named.Obj().Name() != "User" {
					t.Errorf("Expected type name User, got %s", named.Obj().Name())
				}
			} else {
				t.Errorf("User should be a named type, got %T", obj.Type())
			}
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
			if obj.Type().String() != "string" {
				t.Errorf("parameter 'name' should be string, got %s", obj.Type())
			}
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
			if obj.Type().String() != "string" {
				t.Errorf("parameter 'title' should be string, got %s", obj.Type())
			}
		} else {
			t.Error("parameter 'title' should be in scope")
		}

		if obj := scope.Lookup("count"); obj != nil {
			if obj.Type().String() != "int" {
				t.Errorf("parameter 'count' should be int, got %s", obj.Type())
			}
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

		// Check for loop variables - they should have proper types
		if obj := forScope.Lookup("i"); obj != nil {
			if obj.Type().String() != "int" {
				t.Errorf("loop variable 'i' should be int, got %s", obj.Type())
			}
		} else {
			t.Error("loop variable 'i' should be in for scope")
		}

		if obj := forScope.Lookup("item"); obj != nil {
			if obj.Type().String() != "string" {
				t.Errorf("loop variable 'item' should be string, got %s", obj.Type())
			}
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
				if typeStr != tc.expected {
					t.Errorf("expected %s to resolve to %s, got %s", tc.expr, tc.expected, typeStr)
				}
			})
		}
	})
}

// TestNestedScopes tests that nested scopes and local variables are properly assigned
func TestNestedScopes(t *testing.T) {
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

	t.Run("NestedScopes", func(t *testing.T) {
		// Find the NestedScopes template
		var nestedTmpl *templparser.HTMLTemplate
		for _, node := range tf.Nodes {
			if tmpl, ok := node.(*templparser.HTMLTemplate); ok {
				if extractTemplateName(tmpl) == "NestedScopes" {
					nestedTmpl = tmpl
					break
				}
			}
		}

		if nestedTmpl == nil {
			t.Fatal("NestedScopes template not found")
		}

		// Check template parameter
		scope := nestedTmpl.Scope().GoScope
		if obj := scope.Lookup("data"); obj != nil {
			if obj.Type().String() != "[]int" {
				t.Errorf("parameter 'data' should be []int, got %s", obj.Type())
			}
		} else {
			t.Error("parameter 'data' should be in scope")
		}

		// Find the for loop
		var forExpr *templparser.ForExpression
		walkTemplateNodes(nestedTmpl.Children, func(n templparser.Node) bool {
			if fe, ok := n.(*templparser.ForExpression); ok {
				forExpr = fe
				return false
			}
			return true
		})

		if forExpr != nil {
			forScope := forExpr.Scope().GoScope
			
			// Check loop variables should have proper types
			if obj := forScope.Lookup("i"); obj != nil {
				if obj.Type().String() != "int" {
					t.Errorf("loop variable 'i' should be int, got %s", obj.Type())
				}
			} else {
				t.Error("loop variable 'i' should be in for scope")
			}

			if obj := forScope.Lookup("val"); obj != nil {
				if obj.Type().String() != "int" {
					t.Errorf("loop variable 'val' should be int, got %s", obj.Type())
				}
			} else {
				t.Error("loop variable 'val' should be in for scope")
			}
		}

		// Find the switch expression
		var switchExpr *templparser.SwitchExpression
		walkTemplateNodes(nestedTmpl.Children, func(n templparser.Node) bool {
			if se, ok := n.(*templparser.SwitchExpression); ok {
				switchExpr = se
				return false
			}
			return true
		})

		if switchExpr != nil {
			// Each case has its own scope
			for i, caseNode := range switchExpr.Cases {
				if caseNode.Scope() == nil {
					t.Errorf("case %d should have a scope", i)
				}
			}
		}
		
		// Test local variable resolution
		t.Log("Testing local variable resolution...")
		
		// Get the template scope - this includes local variables from overlay
		tmplScope := nestedTmpl.Scope().GoScope
		
		// Verify we can resolve the 'total' local variable
		totalExpr, err := parser.ParseExpr("total")
		if err != nil {
			t.Fatalf("Failed to parse 'total': %v", err)
		}
		
		totalType, err := ResolveExpression(totalExpr, tmplScope)
		if err != nil {
			t.Errorf("Failed to resolve local variable 'total': %v", err)
		} else {
			if totalType.String() != "int" {
				t.Errorf("Expected 'total' to be int, got %s", totalType)
			} else {
				t.Log("Successfully resolved local variable 'total' to int")
			}
		}
		
		// Test resolving from nested scope
		if forExpr != nil && forExpr.Scope() != nil {
			forScope := forExpr.Scope().GoScope
			
			// Should be able to resolve parent scope variables from nested scope
			totalFromFor, err := ResolveExpression(totalExpr, forScope)
			if err != nil {
				t.Errorf("Failed to resolve 'total' from for scope: %v", err)
			} else {
				if totalFromFor.String() != "int" {
					t.Errorf("Expected 'total' from for scope to be int, got %s", totalFromFor)
				} else {
					t.Log("Successfully resolved 'total' from within for loop scope")
				}
			}
		}
	})

	t.Run("TypeSwitchScopes", func(t *testing.T) {
		// Find the TypeSwitchScopes template
		var typeSwitchTmpl *templparser.HTMLTemplate
		for _, node := range tf.Nodes {
			if tmpl, ok := node.(*templparser.HTMLTemplate); ok {
				if extractTemplateName(tmpl) == "TypeSwitchScopes" {
					typeSwitchTmpl = tmpl
					break
				}
			}
		}

		if typeSwitchTmpl == nil {
			t.Fatal("TypeSwitchScopes template not found")
		}

		// Find the switch expression
		var switchExpr *templparser.SwitchExpression
		walkTemplateNodes(typeSwitchTmpl.Children, func(n templparser.Node) bool {
			if se, ok := n.(*templparser.SwitchExpression); ok {
				switchExpr = se
				return false
			}
			return true
		})

		if switchExpr == nil {
			t.Fatal("Switch expression not found")
		}

		// Also check the type switch variable 'v' in each case
		for i, caseNode := range switchExpr.Cases {
			if caseNode.Scope() != nil && caseNode.Scope().GoScope != nil {
				if obj := caseNode.Scope().GoScope.Lookup("v"); obj != nil {
					// Check the type of 'v' in each case
					switch i {
					case 0: // string case
						if obj.Type().String() != "string" {
							t.Errorf("type switch variable 'v' in string case should be string, got %s", obj.Type())
						}
					case 1: // int case
						if obj.Type().String() != "int" {
							t.Errorf("type switch variable 'v' in int case should be int, got %s", obj.Type())
						}
					case 2: // []string case
						if obj.Type().String() != "[]string" {
							t.Errorf("type switch variable 'v' in []string case should be []string, got %s", obj.Type())
						}
					}
				} else if i < 3 { // First 3 cases should have 'v'
					t.Errorf("type switch variable 'v' should be in case %d scope", i)
				}
			}
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
				// Component functions should return templ.Component
				if sig, ok := compType.(*types.Signature); ok {
					results := sig.Results()
					if results.Len() == 1 {
						returnType := results.At(0).Type().String()
						if returnType != "github.com/a-h/templ.Component" {
							t.Errorf("component %s should return templ.Component, got %s", elem.Name, returnType)
						}
					} else {
						t.Errorf("component %s should have 1 return value, got %d", elem.Name, results.Len())
					}
				} else {
					t.Errorf("component %s should be a function, got %T", elem.Name, compType)
				}
			}

			// Test resolving attributes
			for _, attr := range elem.Attributes {
				if ea, ok := attr.(*templparser.ExpressionAttribute); ok {
					attrExpr, err := parser.ParseExpr(ea.Expression.Value)
					if err == nil {
						attrType, err := ResolveExpression(attrExpr, elem.Scope().GoScope)
						if err != nil {
							// Some attributes may fail to resolve (e.g., complex expressions)
							t.Logf("Failed to resolve attribute %s = { %s }: %v",
								ea.Key.String(), ea.Expression.Value, err)
						} else {
							// Validate known attribute types
							switch ea.Key.String() {
							case "name":
								if attrType.String() != "string" {
									t.Errorf("attribute 'name' should be string, got %s", attrType)
								}
							case "title":
								if attrType.String() != "string" {
									t.Errorf("attribute 'title' should be string, got %s", attrType)
								}
							case "count":
								if attrType.String() != "int" {
									t.Errorf("attribute 'count' should be int, got %s", attrType)
								}
							}
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