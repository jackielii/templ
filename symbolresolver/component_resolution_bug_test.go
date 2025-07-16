package symbolresolver

import (
	"os"
	"path/filepath"
	"testing"
	templparser "github.com/a-h/templ/parser/v2"
)

// TestComponentResolutionBug records a bug where local components in the same file
// are not being recognized as valid components due to "invalid type" in the overlay
func TestComponentResolutionBug(t *testing.T) {
	// Create a temporary directory with proper module structure
	tempDir, err := os.MkdirTemp("", "templ-component-bug-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Write test file with local component usage
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
	
	// Get the current working directory (should be the templ repo root)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	
	// Write a go.mod file to make it a proper module
	// Use replace directive to point to the local templ package
	goMod := fmt.Sprintf(`module test

go 1.19

require github.com/a-h/templ v0.3.0

replace github.com/a-h/templ => %s
`, filepath.Dir(cwd))
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
	
	// Debug: Check the generated overlay content
	overlayContent, err := resolver.GetOverlayContent(testFile)
	if err != nil {
		t.Fatalf("failed to get overlay content: %v", err)
	}
	t.Logf("Generated overlay content:\n%s", overlayContent)

	// Verify Component function is loaded in package scope
	if pkg, ok := resolver.packages[filepath.Dir(testFile)]; ok {
		if pkg.Types != nil && pkg.Types.Scope() != nil {
			names := pkg.Types.Scope().Names()
			hasComponent := false
			for _, name := range names {
				if name == "Component" {
					hasComponent = true
					obj := pkg.Types.Scope().Lookup("Component")
					if obj != nil {
						t.Logf("Component function found with type: %s", obj.Type().String())
						// BUG: This should be a valid function type, not "invalid type"
						if obj.Type().String() == "invalid type" {
							t.Errorf("BUG: Component function has invalid type - overlay generation issue")
						}
					}
					break
				}
			}
			if !hasComponent {
				t.Errorf("BUG: Component function not found in package scope")
			}
		}
	} else {
		t.Errorf("BUG: Package not loaded in resolver")
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

	// This is the main bug - Component should be recognized as a component
	if !componentElem.IsComponent() {
		t.Errorf("BUG: Component element should be recognized as a component but isn't")
		
		// Additional debugging
		if scope := componentElem.Scope(); scope != nil && scope.GoScope != nil {
			_, obj := scope.GoScope.LookupParent("Component", 0)
			if obj != nil {
				t.Logf("Component found via LookupParent: %s", obj.Type().String())
				// This should trigger the validation failure
				if obj.Type().String() == "invalid type" {
					t.Logf("Root cause: Component function has invalid type due to overlay generation issue")
				}
			}
		}
	}
}