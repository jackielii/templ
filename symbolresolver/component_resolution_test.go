package symbolresolver

import (
	"path/filepath"
	"testing"

	templparser "github.com/a-h/templ/parser/v2"
)

func TestComponentResolution(t *testing.T) {
	// Use the existing test file
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
		t.Fatalf("failed to assign scopes: %v", err)
	}

	// Find the ParentComponent template which uses elements
	var parentTmpl *templparser.HTMLTemplate
	for _, node := range tf.Nodes {
		if tmpl, ok := node.(*templparser.HTMLTemplate); ok {
			name := extractTemplateName(tmpl)
			if name == "ParentComponent" {
				parentTmpl = tmpl
				break
			}
		}
	}

	if parentTmpl == nil {
		t.Fatal("ParentComponent template not found")
	}

	// Collect all elements
	elements := make(map[string]*templparser.Element)
	walkTemplateNodes(parentTmpl.Children, func(n templparser.Node) bool {
		if elem, ok := n.(*templparser.Element); ok {
			elements[elem.Name] = elem
		}
		return true
	})

	// Test expectations
	testCases := []struct {
		name        string
		shouldExist bool
		isComponent bool
	}{
		{"div", true, false},               // Regular HTML element
		{"SimpleTemplate", true, true},     // Local component (appears twice)
		{"WithMultipleParams", true, true}, // Another local component
	}

	for _, tc := range testCases {
		elem, exists := elements[tc.name]
		if exists != tc.shouldExist {
			t.Errorf("Element %s: expected to exist=%v, got %v", tc.name, tc.shouldExist, exists)
			continue
		}

		if !exists {
			continue
		}

		if elem.IsComponent() != tc.isComponent {
			t.Errorf("Element %s: expected IsComponent=%v, got %v", tc.name, tc.isComponent, elem.IsComponent())
		}

		t.Logf("Element %s: IsComponent=%v, Scope=%v", tc.name, elem.IsComponent(), elem.Scope() != nil)
	}
}