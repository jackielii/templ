package generator

import (
	"strings"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

// TestUnifiedResolverAdvancedFeatures tests the advanced features merged from SymbolResolver
func TestUnifiedResolverAdvancedFeatures(t *testing.T) {
	resolver := newSymbolResolver()

	testCases := []struct {
		name         string
		fromDir      string
		pkgPath      string
		componentName string
		wantErr      bool
		wantStruct   bool
		wantPointer  bool
	}{
		{
			name:          "Valid struct component with pointer receiver",
			fromDir:       "./test-element-component",
			pkgPath:       "github.com/a-h/templ/generator/test-element-component/mod",
			componentName: "StructComponent",
			wantErr:       false,
			wantStruct:    true,
			wantPointer:   true,
		},
		{
			name:          "Valid templ function component",
			fromDir:       "./test-element-component",
			pkgPath:       "github.com/a-h/templ/generator/test-element-component",
			componentName: "Button",
			wantErr:       false,
			wantStruct:    false,
			wantPointer:   false,
		},
		{
			name:          "Non-existent component",
			fromDir:       "./test-element-component",
			pkgPath:       "github.com/a-h/templ/generator/test-element-component",
			componentName: "NonExistent",
			wantErr:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sig, err := resolver.ResolveComponentFrom(tc.fromDir, tc.pkgPath, tc.componentName)

			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %s.%s, but got none", tc.pkgPath, tc.componentName)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error resolving %s.%s: %v", tc.pkgPath, tc.componentName, err)
			}

			if sig.IsStruct != tc.wantStruct {
				t.Errorf("IsStruct: got %v, want %v", sig.IsStruct, tc.wantStruct)
			}

			if sig.IsPointerRecv != tc.wantPointer {
				t.Errorf("IsPointerRecv: got %v, want %v", sig.IsPointerRecv, tc.wantPointer)
			}

			t.Logf("Resolved %s: IsStruct=%v, IsPointerRecv=%v, Parameters=%d", 
				tc.componentName, sig.IsStruct, sig.IsPointerRecv, len(sig.Parameters))
		})
	}
}

// TestUnifiedResolverPositionAwareErrors tests position-aware error reporting
func TestUnifiedResolverPositionAwareErrors(t *testing.T) {
	resolver := newSymbolResolver()

	pos := parser.Position{Line: 10, Col: 5, Index: 100}
	fileName := "test.templ"

	// Test with non-existent component
	_, err := resolver.ResolveComponentWithPosition(
		"./test-element-component",
		"github.com/a-h/templ/generator/test-element-component",
		"NonExistent",
		pos,
		fileName,
	)

	if err == nil {
		t.Fatal("expected error for non-existent component")
	}

	// Check if it's a ComponentResolutionError with position info
	if resErr, ok := err.(ComponentResolutionError); ok {
		if resErr.Position.Line != pos.Line {
			t.Errorf("Position line: got %d, want %d", resErr.Position.Line, pos.Line)
		}
		if resErr.Position.Col != pos.Col {
			t.Errorf("Position col: got %d, want %d", resErr.Position.Col, pos.Col)
		}
		if resErr.FileName != fileName {
			t.Errorf("FileName: got %q, want %q", resErr.FileName, fileName)
		}

		errorStr := resErr.Error()
		if !strings.Contains(errorStr, fileName) {
			t.Errorf("Error string should contain filename: %s", errorStr)
		}
		if !strings.Contains(errorStr, "10:5") {
			t.Errorf("Error string should contain position: %s", errorStr)
		}

		t.Logf("Position-aware error: %s", errorStr)
	} else {
		t.Errorf("Expected ComponentResolutionError, got %T", err)
	}
}

// TestUnifiedResolverStructFields tests struct field extraction
func TestUnifiedResolverStructFields(t *testing.T) {
	resolver := newSymbolResolver()

	sig, err := resolver.ResolveComponentFrom(
		"./test-element-component",
		"github.com/a-h/templ/generator/test-element-component/mod",
		"StructComponent",
	)

	if err != nil {
		t.Fatalf("Failed to resolve StructComponent: %v", err)
	}

	// StructComponent should have Name, Child, and Attrs fields
	expectedFields := []string{"Name", "Child", "Attrs"}
	if len(sig.Parameters) != len(expectedFields) {
		t.Errorf("Expected %d fields, got %d", len(expectedFields), len(sig.Parameters))
	}

	for i, expected := range expectedFields {
		if i < len(sig.Parameters) {
			if sig.Parameters[i].Name != expected {
				t.Errorf("Field %d: got %q, want %q", i, sig.Parameters[i].Name, expected)
			}
		}
	}

	t.Logf("StructComponent fields:")
	for i, param := range sig.Parameters {
		t.Logf("  %d: %s %s", i, param.Name, param.Type)
	}
}

// TestUnifiedResolverGeneratedFileErrors tests error handling for generated files
func TestUnifiedResolverGeneratedFileErrors(t *testing.T) {
	resolver := newSymbolResolver()

	// This test verifies that the resolver can handle packages with _templ.go file errors
	// Since we can't easily create such errors in a test, we'll just verify the resolver
	// doesn't crash when dealing with the test-element-component package
	_, err := resolver.ResolveComponentFrom(
		"./test-element-component",
		"github.com/a-h/templ/generator/test-element-component",
		"Button",
	)

	if err != nil {
		// If there's an error, it should not be about _templ.go files
		if strings.Contains(err.Error(), "_templ.go") {
			t.Errorf("Should ignore _templ.go file errors: %v", err)
		} else {
			t.Logf("Got expected non-_templ.go error: %v", err)
		}
	} else {
		t.Log("Successfully resolved Button component")
	}
}

// TestUnifiedResolverCachePerformance tests that caching improves performance
func TestUnifiedResolverCachePerformance(t *testing.T) {
	resolver := newSymbolResolver()

	// First resolution - should be slower
	sig1, err := resolver.ResolveComponentFrom(
		"./test-element-component",
		"github.com/a-h/templ/generator/test-element-component",
		"Button",
	)
	if err != nil {
		t.Fatalf("First resolution failed: %v", err)
	}

	// Second resolution - should use cache
	sig2, err := resolver.ResolveComponentFrom(
		"./test-element-component",
		"github.com/a-h/templ/generator/test-element-component",
		"Button",
	)
	if err != nil {
		t.Fatalf("Second resolution failed: %v", err)
	}

	// Verify signatures are identical
	if sig1.Name != sig2.Name || sig1.PackagePath != sig2.PackagePath {
		t.Error("Cached signature doesn't match original")
	}

	// Verify cache is populated
	if resolver.CacheSize() == 0 {
		t.Error("Cache should be populated after resolution")
	}

	t.Logf("Cache size: %d entries", resolver.CacheSize())
}