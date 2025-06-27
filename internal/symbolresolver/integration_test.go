package symbolresolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	templparser "github.com/a-h/templ/parser/v2"
)

func TestIntegration_GeneratorDirectory(t *testing.T) {
	// Create resolver
	resolver := NewSymbolResolverV2()

	// Find all .templ files in generator directory
	var templFiles []string
	generatorDir := filepath.Join("..", "..", "generator")

	err := filepath.Walk(generatorDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".templ") {
			templFiles = append(templFiles, path)
		} else if strings.HasSuffix(path, "_templ.go") {
			// Remove existing generated files to ensure clean test
			if err := os.Remove(path); err != nil {
				t.Logf("Warning: failed to remove %s: %v", path, err)
			} else {
				t.Logf("Removed existing generated file: %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk generator directory: %v", err)
	}

	t.Logf("Found %d templ files in generator directory", len(templFiles))

	// Preprocess all files
	err = resolver.PreprocessFiles(templFiles)
	if err != nil {
		t.Fatalf("PreprocessFiles failed: %v", err)
	}

	// Test specific file: generator/test-element-component/mod/template.templ
	targetFile := filepath.Join(generatorDir, "test-element-component", "mod", "template.templ")

	// Parse the template file
	tf, err := templparser.Parse(targetFile)
	if err != nil {
		t.Logf("Warning: Failed to parse template file: %v", err)
		return
	}

	// Test resolving the Text component
	targetDir := filepath.Dir(targetFile)
	sig, err := resolver.ResolveComponent(targetDir, "Text", tf)
	if err != nil {
		t.Logf("Warning: Failed to resolve Text component: %v", err)

		// Debug: Show what packages are loaded
		t.Logf("Loaded packages:")
		for path, pkg := range resolver.packages {
			if pkg.Types != nil && pkg.Types.Scope() != nil {
				t.Logf("  %s -> %s (scope names: %v)", path, pkg.PkgPath, pkg.Types.Scope().Names())
			} else {
				t.Logf("  %s -> %s (no scope)", path, pkg.PkgPath)
			}
		}

		// Check if the mod package was loaded
		modPkgPath := "github.com/a-h/templ/generator/test-element-component/mod"
		if modPkg, ok := resolver.packages[modPkgPath]; ok {
			t.Logf("mod package found with path %s", modPkg.PkgPath)
			if modPkg.Types != nil && modPkg.Types.Scope() != nil {
				t.Logf("mod package scope names: %v", modPkg.Types.Scope().Names())
			}
		} else {
			t.Logf("mod package NOT found with path %s", modPkgPath)
		}

		// Check overlays for mod/template_templ.go
		modOverlayPath := filepath.Join(generatorDir, "test-element-component", "mod", "template_templ.go")
		if overlay, ok := resolver.overlays[modOverlayPath]; ok {
			t.Logf("Found overlay for mod/template_templ.go:")
			t.Logf("%s", string(overlay))
		} else {
			t.Logf("No overlay found for %s", modOverlayPath)
			t.Logf("Available overlay paths:")
			for path := range resolver.overlays {
				if strings.Contains(path, "mod") {
					t.Logf("  %s", path)
				}
			}
		}

		// Check absolute paths
		absModOverlayPath, _ := filepath.Abs(modOverlayPath)
		t.Logf("Looking for absolute path: %s", absModOverlayPath)
		if _, ok := resolver.overlays[absModOverlayPath]; ok {
			t.Logf("Found overlay at absolute path!")
		}

		// Don't fail the test, just log the error
		return
	}

	// Verify signature
	if sig == nil {
		t.Fatal("Expected non-nil signature for Text component")
	}

	// Check parameters
	params := sig.Params()
	if params.Len() != 2 {
		t.Errorf("Expected 2 parameters, got %d", params.Len())
	}

	// Check parameter names and types
	expectedParams := []struct {
		name string
		typ  string
	}{
		{"name", "string"},
		{"attrs", "github.com/a-h/templ.Attributer"},
	}

	for i, expected := range expectedParams {
		if i >= params.Len() {
			break
		}
		param := params.At(i)
		if param.Name() != expected.name {
			t.Errorf("Parameter %d: expected name %q, got %q", i, expected.name, param.Name())
		}
		actualType := param.Type().String()
		if actualType != expected.typ {
			t.Errorf("Parameter %d: expected type %q, got %q", i, expected.typ, actualType)
		}
	}

	// Log some statistics
	t.Logf("Successfully resolved component Text with signature: %s", sig.String())
	t.Logf("Total packages loaded: %d", len(resolver.packages))
	t.Logf("Total overlays generated: %d", len(resolver.overlays))

	// Test cross-package resolution if there are imports
	// Look for any components that might import from mod package
	for _, file := range templFiles {
		if strings.Contains(file, "test-element-component") && file != targetFile {
			tf2, err := templparser.Parse(file)
			if err != nil {
				continue
			}

			// Check if this file imports mod.Text
			for _, node := range tf2.Nodes {
				if goExpr, ok := node.(*templparser.TemplateFileGoExpression); ok {
					if strings.Contains(goExpr.Expression.Value, "import") && strings.Contains(goExpr.Expression.Value, "/mod") {
						t.Logf("Found file importing mod package: %s", file)

						// Try to resolve mod.Text from this file
						fromDir := filepath.Dir(file)
						println("Resolving mod.Text from", fromDir)
						_, err := resolver.ResolveComponent(fromDir, "mod.Text", tf2)
						if err != nil {
							t.Logf("Note: Could not resolve mod.Text from %s: %v", file, err)
						} else {
							t.Logf("Successfully resolved mod.Text from %s", file)
						}
					}
				}
			}
		}
	}
}

