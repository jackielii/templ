package generator

import (
	"testing"

	"github.com/a-h/templ/parser/v2"
)

// TestUnifiedResolverLocalTemplateExtraction tests that the unified resolver
// correctly extracts local template signatures
func TestUnifiedResolverLocalTemplateExtraction(t *testing.T) {
	resolver := newSymbolResolver()

	// Template with multiple local components
	templContent := `package main

templ Button(title string) {
	<button>{ title }</button>
}

templ Card(title string, content string) {
	<div class="card">
		<h2>{ title }</h2>
		<p>{ content }</p>
	</div>
}

type MyComponent struct {
	Name string
}

func (c MyComponent) Render(ctx context.Context, w io.Writer) error {
	return nil
}
`

	// Parse the template
	tf, err := parser.ParseString(templContent)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Extract signatures
	resolver.ExtractSignatures(tf)

	// Test Button component
	buttonSig, ok := resolver.GetLocalTemplate("Button")
	if !ok {
		t.Error("Button component not found in local templates")
	} else {
		if buttonSig.Name != "Button" {
			t.Errorf("Button component name: got %q, want %q", buttonSig.Name, "Button")
		}
		if len(buttonSig.Parameters) != 1 {
			t.Errorf("Button parameters: got %d, want 1", len(buttonSig.Parameters))
		} else if buttonSig.Parameters[0].Name != "title" {
			t.Errorf("Button parameter name: got %q, want %q", buttonSig.Parameters[0].Name, "title")
		}
	}

	// Test Card component
	cardSig, ok := resolver.GetLocalTemplate("Card")
	if !ok {
		t.Error("Card component not found in local templates")
	} else {
		if cardSig.Name != "Card" {
			t.Errorf("Card component name: got %q, want %q", cardSig.Name, "Card")
		}
		if len(cardSig.Parameters) != 2 {
			t.Errorf("Card parameters: got %d, want 2", len(cardSig.Parameters))
		}
	}

	// Test struct component
	structSig, ok := resolver.GetLocalTemplate("MyComponent")
	if !ok {
		t.Error("MyComponent struct not found in local templates")
	} else {
		if structSig.Name != "MyComponent" {
			t.Errorf("MyComponent name: got %q, want %q", structSig.Name, "MyComponent")
		}
		if !structSig.IsStruct {
			t.Error("MyComponent should be marked as struct")
		}
	}

	// Test alias functionality
	resolver.AddLocalTemplateAlias("ButtonAlias", "Button")
	aliasSig, ok := resolver.GetLocalTemplate("ButtonAlias")
	if !ok {
		t.Error("ButtonAlias not found")
	} else if aliasSig.Name != "Button" {
		t.Errorf("ButtonAlias should resolve to Button component")
	}

	// Test GetAllLocalTemplateNames
	names := resolver.GetAllLocalTemplateNames()
	expectedNames := []string{"Button", "Card", "MyComponent", "ButtonAlias"}
	if len(names) != len(expectedNames) {
		t.Errorf("Local template names: got %d, want %d", len(names), len(expectedNames))
	}

	t.Logf("Local templates found: %v", names)
}

// TestUnifiedResolverComponentSignatureStorage tests the component signature storage
func TestUnifiedResolverComponentSignatureStorage(t *testing.T) {
	resolver := newSymbolResolver()

	// Create a test signature
	sig := ComponentSignature{
		PackagePath:   "example.com/pkg",
		Name:          "TestComponent",
		QualifiedName: "pkg.TestComponent",
		Parameters: []ParameterInfo{
			{Name: "param1", Type: "string"},
			{Name: "param2", Type: "int"},
		},
	}

	// Add the signature
	resolver.AddComponentSignature(sig)

	// Retrieve the signature
	retrievedSig, ok := resolver.GetComponentSignature("pkg.TestComponent")
	if !ok {
		t.Error("Component signature not found")
	}

	// Verify the signature
	if retrievedSig.Name != "TestComponent" {
		t.Errorf("Component name: got %q, want %q", retrievedSig.Name, "TestComponent")
	}
	if retrievedSig.PackagePath != "example.com/pkg" {
		t.Errorf("Package path: got %q, want %q", retrievedSig.PackagePath, "example.com/pkg")
	}
	if len(retrievedSig.Parameters) != 2 {
		t.Errorf("Parameters count: got %d, want 2", len(retrievedSig.Parameters))
	}

	t.Logf("Component signature stored and retrieved successfully: %+v", retrievedSig)
}

// TestUnifiedResolverClearAllCaches tests that cache clearing works for all caches
func TestUnifiedResolverClearAllCaches(t *testing.T) {
	resolver := newSymbolResolver()

	// Add some data to various caches
	templContent := `package main
templ Button(title string) {
	<button>{ title }</button>
}`

	tf, _ := parser.ParseString(templContent)
	resolver.ExtractSignatures(tf)

	sig := ComponentSignature{
		PackagePath:   "example.com/pkg",
		Name:          "TestComponent",
		QualifiedName: "pkg.TestComponent",
	}
	resolver.AddComponentSignature(sig)

	// Verify data is there
	if _, ok := resolver.GetLocalTemplate("Button"); !ok {
		t.Error("Button should be in local templates before clear")
	}
	if _, ok := resolver.GetComponentSignature("pkg.TestComponent"); !ok {
		t.Error("TestComponent should be in component signatures before clear")
	}

	// Clear cache
	resolver.ClearCache()

	// Verify data is gone
	if _, ok := resolver.GetLocalTemplate("Button"); ok {
		t.Error("Button should not be in local templates after clear")
	}
	if _, ok := resolver.GetComponentSignature("pkg.TestComponent"); ok {
		t.Error("TestComponent should not be in component signatures after clear")
	}

	t.Log("Cache clearing works correctly")
}