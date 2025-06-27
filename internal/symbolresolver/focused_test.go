package symbolresolver

import (
	"path/filepath"
	"testing"

	templparser "github.com/a-h/templ/parser/v2"
)

func TestResolveComponent_SinglePackage(t *testing.T) {
	// Create resolver
	resolver := NewSymbolResolverV2()

	// Find a specific templ file
	testFile, err := filepath.Abs(filepath.Join("..", "..", "generator", "test-element-component", "template.templ"))
	if err != nil {
		t.Fatal(err)
	}

	// Preprocess just this file
	err = resolver.PreprocessFiles([]string{testFile})
	if err != nil {
		t.Fatalf("PreprocessFiles failed: %v", err)
	}

	// Parse the template file
	tf, err := templparser.Parse(testFile)
	if err != nil {
		t.Fatal(err)
	}

	// Test resolving a local component
	sig, err := resolver.ResolveComponent(filepath.Dir(testFile), "Button", tf)
	if err != nil {
		t.Errorf("Failed to resolve component Button: %v", err)
		
		// Debug output
		t.Logf("Packages in cache: %d", len(resolver.packages))
		for key, pkg := range resolver.packages {
			t.Logf("  %s -> %s (Types: %v)", key, pkg.PkgPath, pkg.Types != nil)
			if pkg.Types != nil && pkg.Types.Scope() != nil {
				t.Logf("    Scope names: %v", pkg.Types.Scope().Names())
			}
		}
		return
	}

	// Check signature
	if sig == nil {
		t.Error("Expected non-nil signature")
		return
	}

	params := sig.Params()
	if params.Len() != 1 {
		t.Errorf("Expected 1 parameter, got %d", params.Len())
	}
}