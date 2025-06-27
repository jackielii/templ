package symbolresolver

import (
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	templparser "github.com/a-h/templ/parser/v2"
	"golang.org/x/tools/go/packages"
)

func TestIntegration_GeneratorDirectory(t *testing.T) {
	// Create resolver
	resolver := NewSymbolResolverV2()

	// Find all .templ files in generator directory
	var templFiles []string
	generatorDir, err := filepath.Abs(filepath.Join("..", "..", "generator"))
	if err != nil {
		t.Fatalf("Failed to get absolute path for generator directory: %v", err)
	}

	err = filepath.Walk(generatorDir, func(path string, info os.FileInfo, err error) error {
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

	// Test resolving various component patterns
	testCases := []struct {
		name           string
		componentName  string
		fromDir        string
		expectedParams []struct {
			name string
			typ  string
		}
		shouldFail bool
	}{
		{
			name:          "Local Text component",
			componentName: "Text",
			fromDir:       filepath.Dir(targetFile),
			expectedParams: []struct {
				name string
				typ  string
			}{
				{"name", "string"},
				{"attrs", "github.com/a-h/templ.Attributer"},
			},
		},
		{
			name:          "Button component",
			componentName: "Button",
			fromDir:       filepath.Dir(filepath.Join(generatorDir, "test-element-component", "template.templ")),
			expectedParams: []struct {
				name string
				typ  string
			}{
				{"title", "string"},
			},
		},
		{
			name:          "NoArgsComponent",
			componentName: "NoArgsComponent",
			fromDir:       filepath.Dir(filepath.Join(generatorDir, "test-element-component", "template.templ")),
			expectedParams: []struct {
				name string
				typ  string
			}{},
		},
		{
			name:          "BoolComponent with multiple params",
			componentName: "BoolComponent",
			fromDir:       filepath.Dir(filepath.Join(generatorDir, "test-element-component", "template.templ")),
			expectedParams: []struct {
				name string
				typ  string
			}{
				{"title", "string"},
				{"enabled", "bool"},
				{"attrs", "github.com/a-h/templ.Attributer"},
			},
		},
		{
			name:          "MultiComponent with mixed types",
			componentName: "MultiComponent",
			fromDir:       filepath.Dir(filepath.Join(generatorDir, "test-element-component", "template.templ")),
			expectedParams: []struct {
				name string
				typ  string
			}{
				{"title", "string"},
				{"count", "int"},
				{"enabled", "bool"},
				{"visible", "bool"},
			},
		},
		{
			name:          "Container with templ.Component param",
			componentName: "Container",
			fromDir:       filepath.Dir(filepath.Join(generatorDir, "test-element-component", "template.templ")),
			expectedParams: []struct {
				name string
				typ  string
			}{
				{"child", "github.com/a-h/templ.Component"},
			},
		},
		{
			name:          "Cross-package mod.Text",
			componentName: "mod.Text",
			fromDir:       filepath.Dir(filepath.Join(generatorDir, "test-element-component", "template.templ")),
			expectedParams: []struct {
				name string
				typ  string
			}{
				{"name", "string"},
				{"attrs", "github.com/a-h/templ.Attributer"},
			},
		},
		// TODO: extern.Text requires cross-module resolution which is more complex
		// {
		// 	name:          "Cross-package extern.Text",
		// 	componentName: "extern.Text",
		// 	fromDir:       filepath.Dir(filepath.Join(generatorDir, "test-element-component", "template.templ")),
		// 	expectedParams: []struct {
		// 		name string
		// 		typ  string
		// 	}{
		// 		{"name", "string"},
		// 	},
		// },
		{
			name:          "Struct component mod.StructComponent",
			componentName: "mod.StructComponent",
			fromDir:       filepath.Dir(filepath.Join(generatorDir, "test-element-component", "template.templ")),
			expectedParams: []struct {
				name string
				typ  string
			}{
				// StructComponent implements templ.Component directly
				{"ctx", "context.Context"},
				{"w", "io.Writer"},
			},
		},
		{
			name:          "Non-existent component",
			componentName: "NonExistent",
			fromDir:       filepath.Dir(filepath.Join(generatorDir, "test-element-component", "template.templ")),
			shouldFail:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Get the template file for the fromDir
			var tf *templparser.TemplateFile
			for _, file := range templFiles {
				if filepath.Dir(file) == tc.fromDir {
					parsedTf, err := templparser.Parse(file)
					if err == nil {
						tf = parsedTf
						break
					}
				}
			}
			if tf == nil && !tc.shouldFail {
				t.Skip("No template file found for directory")
			}

			sig, err := resolver.ResolveComponent(tc.fromDir, tc.componentName, tf)

			if tc.shouldFail {
				if err == nil {
					t.Errorf("Expected error for component %s, but got none", tc.componentName)
				}
				return
			}

			if err != nil {
				t.Errorf("Failed to resolve component %s: %v", tc.componentName, err)

				// Debug info for failed cases
				if tc.componentName == "extern.Text" {
					t.Logf("Debug: Looking for extern.Text")
					// Check if extern package is loaded
					externPkgPath := "github.com/a-h/templ/generator/test-element-component/externmod"
					if externPkg, ok := resolver.packages[externPkgPath]; ok {
						t.Logf("externmod package found: %s", externPkg.PkgPath)
						if len(externPkg.Errors) > 0 {
							t.Logf("externmod package has errors:")
							for _, err := range externPkg.Errors {
								t.Logf("  - %v", err)
							}
						}
						if externPkg.Types != nil && externPkg.Types.Scope() != nil {
							t.Logf("externmod scope names: %v", externPkg.Types.Scope().Names())
						}
						t.Logf("externmod GoFiles: %v", externPkg.GoFiles)
					} else {
						t.Logf("externmod package NOT found with path %s", externPkgPath)
					}

					// Check overlay for externmod
					externOverlayPath := filepath.Join(generatorDir, "test-element-component", "externmod", "template_templ.go")
					absExternOverlayPath, _ := filepath.Abs(externOverlayPath)
					if overlay, ok := resolver.overlays[absExternOverlayPath]; ok {
						t.Logf("Found overlay for externmod at %s", absExternOverlayPath)
						t.Logf("Overlay content:\n%s", string(overlay))
					} else {
						t.Logf("No overlay found for externmod at %s", absExternOverlayPath)
					}
				}
				return
			}

			if sig == nil {
				t.Errorf("Expected non-nil signature for component %s", tc.componentName)
				return
			}

			// Check parameters
			params := sig.Params()
			if params.Len() != len(tc.expectedParams) {
				t.Errorf("Component %s: expected %d parameters, got %d", tc.componentName, len(tc.expectedParams), params.Len())
			}

			// Check each parameter
			for i, expected := range tc.expectedParams {
				if i >= params.Len() {
					break
				}
				param := params.At(i)
				if param.Name() != expected.name {
					t.Errorf("Component %s parameter %d: expected name %q, got %q", tc.componentName, i, expected.name, param.Name())
				}
				actualType := param.Type().String()
				if actualType != expected.typ {
					t.Errorf("Component %s parameter %d: expected type %q, got %q", tc.componentName, i, expected.typ, actualType)
				}
			}

			// Also check return type
			results := sig.Results()
			if results.Len() > 0 {
				resultType := results.At(0).Type().String()
				// For Render methods, expect error return
				if tc.componentName == "mod.StructComponent" && resultType != "error" {
					t.Errorf("Component %s: expected error return type, got %s", tc.componentName, resultType)
				} else if tc.componentName != "mod.StructComponent" && resultType != "github.com/a-h/templ.Component" {
					t.Errorf("Component %s: expected templ.Component return type, got %s", tc.componentName, resultType)
				}
			}
		})
	}

	// Log some statistics and debug info
	t.Logf("Total packages loaded: %d", len(resolver.packages))
	t.Logf("Total overlays generated: %d", len(resolver.overlays))

	// Debug: show what directories are cached
	t.Logf("Cached package directories:")
	for dir, pkg := range resolver.packages {
		if strings.Contains(dir, "test-element-component") {
			t.Logf("  %s -> PkgPath:%s Name:%s (Types: %v)", dir, pkg.PkgPath, pkg.Name, pkg.Types != nil)
			if pkg.Types != nil && pkg.Types.Scope() != nil {
				t.Logf("    Scope names: %v", pkg.Types.Scope().Names())
			}
			t.Logf("    GoFiles: %v", pkg.GoFiles)
			t.Logf("    CompiledGoFiles: %v", pkg.CompiledGoFiles)
		}
	}

	// Also show some overlays
	t.Logf("Sample overlays:")
	count := 0
	for path := range resolver.overlays {
		if strings.Contains(path, "test-element-component") && count < 3 {
			t.Logf("  Overlay: %s", path)
			count++
		}
	}
}

// TestResolveExpression tests expression type resolution
func TestResolveExpression(t *testing.T) {
	// Create a minimal test case for expression resolution
	resolver := NewSymbolResolverV2()

	// Create a test template with various expressions
	testDir := t.TempDir()
	templFile := filepath.Join(testDir, "test.templ")
	content := `package test

import "fmt"

type User struct {
	Name string
	Age  int
	Tags []string
	Meta map[string]interface{}
}

templ ShowUser(user User, enabled bool) {
	<div>{ user.Name }</div>
}`

	err := os.WriteFile(templFile, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Create a go.mod file
	// Compute the path to the templ module root
	templRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	goModContent := fmt.Sprintf(`module test

go 1.21

require github.com/a-h/templ v0.0.0

replace github.com/a-h/templ => %s
`, templRoot)
	err = os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goModContent), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Preprocess
	err = resolver.PreprocessFiles([]string{templFile})
	if err != nil {
		t.Fatal(err)
	}

	// Get the package from cache after preprocessing
	var pkg *packages.Package
	if p, ok := resolver.packages[testDir]; ok {
		pkg = p
	} else {
		// Try absolute path
		absTestDir, _ := filepath.Abs(testDir)
		if p, ok := resolver.packages[absTestDir]; ok {
			pkg = p
		} else {
			// Find by matching directory
			for dir, p := range resolver.packages {
				absDir, _ := filepath.Abs(dir)
				if absDir == absTestDir {
					pkg = p
					break
				}
			}
		}
	}
	if pkg == nil {
		t.Fatal("package not found in cache after preprocessing")
	}

	// Get the ShowUser function scope
	obj := pkg.Types.Scope().Lookup("ShowUser")
	if obj == nil {
		t.Fatal("ShowUser not found in scope")
	}

	fn, ok := obj.(*types.Func)
	if !ok {
		t.Fatal("ShowUser is not a function")
	}

	sig := fn.Type().(*types.Signature)

	// Create a scope with the function parameters
	scope := types.NewScope(pkg.Types.Scope(), token.NoPos, token.NoPos, "ShowUser")
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		scope.Insert(param)
	}

	// Test cases for expression resolution
	testCases := []struct {
		name         string
		exprStr      string
		expectedType string
		shouldFail   bool
	}{
		{
			name:         "Simple identifier - user",
			exprStr:      "user",
			expectedType: "test.User",
		},
		{
			name:         "Simple identifier - enabled",
			exprStr:      "enabled",
			expectedType: "bool",
		},
		{
			name:         "Field access - user.Name",
			exprStr:      "user.Name",
			expectedType: "string",
		},
		{
			name:         "Field access - user.Age",
			exprStr:      "user.Age",
			expectedType: "int",
		},
		{
			name:         "Array access - user.Tags[0]",
			exprStr:      "user.Tags[0]",
			expectedType: "string",
		},
		{
			name:         "String literal",
			exprStr:      `"hello"`,
			expectedType: "string",
		},
		{
			name:         "Int literal",
			exprStr:      "42",
			expectedType: "int",
		},
		{
			name:       "Unknown identifier",
			exprStr:    "unknown",
			shouldFail: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Parse the expression
			expr, err := parser.ParseExpr(tc.exprStr)
			if err != nil {
				t.Fatalf("Failed to parse expression %q: %v", tc.exprStr, err)
			}

			// Resolve the expression type
			typ, err := resolver.ResolveExpression(expr, scope)

			if tc.shouldFail {
				if err == nil {
					t.Errorf("Expected error for expression %q, but got none", tc.exprStr)
				}
				return
			}

			if err != nil {
				t.Errorf("Failed to resolve expression %q: %v", tc.exprStr, err)
				return
			}

			actualType := typ.String()
			if actualType != tc.expectedType {
				t.Errorf("Expression %q: expected type %q, got %q", tc.exprStr, tc.expectedType, actualType)
			}
		})
	}
}
