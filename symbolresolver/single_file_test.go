package symbolresolver

import (
	"os"
	"path/filepath"
	"testing"
	templparser "github.com/a-h/templ/parser/v2"
)

func TestSingleFileComponentResolution(t *testing.T) {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "templ-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Write test file
	testFile := filepath.Join(tempDir, "test.templ")
	template := `package test

templ Component() {
	<div>content</div>
}

templ Test() {
	<Component />
}`

	err = os.WriteFile(testFile, []byte(template), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	
	// Write a go.mod file to make it a proper module
	goMod := `module test

go 1.19

require github.com/a-h/templ v0.3.0
`
	err = os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Parse the file
	tf, err := templparser.Parse(testFile)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Create resolver and preprocess
	resolver := NewSymbolResolverV2()
	err = resolver.PreprocessFiles([]string{testFile})
	if err != nil {
		t.Fatalf("failed to preprocess: %v", err)
	}

	// Debug: Check the generated overlay
	t.Logf("Test file: %s", testFile)
	
	// Debug: Check if package was loaded
	if pkg, ok := resolver.packages[filepath.Dir(testFile)]; ok {
		t.Logf("Package loaded for directory: %s", filepath.Dir(testFile))
		if pkg.Types != nil && pkg.Types.Scope() != nil {
			t.Logf("Package scope has %d entries", pkg.Types.Scope().NumChildren())
			// List all names in the package scope
			names := pkg.Types.Scope().Names()
			t.Logf("Package scope names: %v", names)
		}
	} else {
		t.Log("Package not found in resolver")
	}

	// Assign scopes
	err = resolver.AssignScopes(tf)
	if err != nil {
		t.Fatalf("failed to assign scopes: %v", err)
	}

	// Find the Test template
	var testTmpl *templparser.HTMLTemplate
	for _, node := range tf.Nodes {
		if tmpl, ok := node.(*templparser.HTMLTemplate); ok && tmpl.Expression.Value == "Test()" {
			testTmpl = tmpl
			break
		}
	}

	if testTmpl == nil {
		t.Fatal("Test template not found")
	}

	// Find the Component element
	var componentElem *templparser.Element
	walkTemplateNodes(testTmpl.Children, func(n templparser.Node) bool {
		if elem, ok := n.(*templparser.Element); ok && elem.Name == "Component" {
			componentElem = elem
		}
		return true
	})

	if componentElem == nil {
		t.Fatal("Component element not found")
	}

	if !componentElem.IsComponent() {
		t.Errorf("Component element should be recognized as a component")
		
		// Debug: Check if it has a scope
		if componentElem.Scope() == nil {
			t.Log("Component element has no scope")
		} else {
			t.Log("Component element has a scope")
		}
		
		// Debug: Try to resolve Component manually
		if scope := componentElem.Scope(); scope != nil && scope.GoScope != nil {
			if obj := scope.GoScope.Lookup("Component"); obj != nil {
				t.Logf("Found Component in scope: %v", obj)
			} else {
				t.Log("Component not found in immediate scope")
				// Try LookupParent
				_, obj := scope.GoScope.LookupParent("Component", 0)
				if obj != nil {
					t.Logf("Found Component via LookupParent: %v", obj)
				} else {
					t.Log("Component not found via LookupParent either")
				}
			}
			
			// Debug: Check what the parent scope is
			if parent := scope.GoScope.Parent(); parent != nil {
				t.Logf("Parent scope exists: %v", parent)
			} else {
				t.Log("No parent scope")
			}
		}
	}
}