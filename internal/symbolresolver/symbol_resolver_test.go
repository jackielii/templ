package symbolresolver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	templparser "github.com/a-h/templ/parser/v2"
)

// Test case structure for symbol resolution
type resolverTestCase struct {
	name        string
	templFiles  map[string]string // filename -> content
	goFiles     map[string]string // filename -> content
	resolveExpr string            // expression to resolve
	wantType    string            // expected type string
	wantErr     bool
}

func TestSymbolResolverV2_PreprocessFiles(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string // filename -> templ content
		wantErr bool
	}{
		{
			name: "single template file",
			files: map[string]string{
				"component.templ": `package main

templ Button(text string) {
	<button>{ text }</button>
}`,
			},
			wantErr: false,
		},
		{
			name: "multiple files with imports",
			files: map[string]string{
				"ui/button.templ": `package ui

templ Button(label string) {
	<button>{ label }</button>
}`,
				"main/page.templ": `package main

import "myapp/ui"

templ Page() {
	@ui.Button("Click me")
}`,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)

			// Write test files
			var files []string
			for name, content := range tt.files {
				path := env.writeFile(t, name, content)
				files = append(files, path)
			}

			err := env.resolver.PreprocessFiles(files)
			if (err != nil) != tt.wantErr {
				t.Errorf("PreprocessFiles() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Check that overlays were created
			if !tt.wantErr {
				for _, file := range files {
					overlayPath := strings.TrimSuffix(file, ".templ") + "_templ.go"
					if _, ok := env.resolver.overlays[overlayPath]; !ok {
						t.Errorf("expected overlay for %s", overlayPath)
					}
				}
			}
		})
	}
}

func TestSymbolResolverV2_ResolveComponent(t *testing.T) {
	tests := []struct {
		name          string
		componentName string
		templFile     string
		setupFiles    map[string]string // additional files to set up
		wantParams    []string          // parameter names
		wantErr       bool
		isTypeComponent bool            // true if it's a type that implements Component
	}{
		{
			name:          "local component",
			componentName: "Button",
			templFile: `package main

templ Button(text string, disabled bool) {
	<button disabled?={ disabled }>{ text }</button>
}`,
			wantParams: []string{"text", "disabled"},
			wantErr:    false,
		},
		{
			name:          "component with no params",
			componentName: "EmptyComponent",
			templFile: `package main

templ EmptyComponent() {
	<div>Empty</div>
}`,
			wantParams: []string{},
			wantErr:    false,
		},
		{
			name:          "basic type implementing Component",
			componentName: "intComp",
			templFile: `package main

import (
	"context"
	"fmt"
	"io"
)

type IntComponent int

func (i IntComponent) Render(ctx context.Context, w io.Writer) error {
	_, err := fmt.Fprintf(w, "<span>%d</span>", i)
	return err
}

var intComp IntComponent = 42

templ TestComponent() {
	<div>Test</div>
}`,
			wantErr:         false,
			isTypeComponent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)

			// Set up any additional files
			for name, content := range tt.setupFiles {
				env.writeFile(t, name, content)
			}

			// Parse and preprocess the main template file
			templPath := env.writeFile(t, "test.templ", tt.templFile)

			// Preprocess to generate overlays
			if err := env.resolver.PreprocessFiles([]string{templPath}); err != nil {
				t.Fatalf("PreprocessFiles failed: %v", err)
			}

			// Parse component name as expression
			expr, parseErr := parser.ParseExpr(tt.componentName)
			if parseErr != nil {
				t.Fatalf("Failed to parse component name %q: %v", tt.componentName, parseErr)
			}

			// Get the file scope first
			fileScope, err := env.resolver.GetFileScope(templPath)
			if err != nil && !tt.wantErr {
				t.Fatalf("Failed to get file scope: %v", err)
			}
			
			// Resolve the component
			var typ types.Type
			if fileScope != nil {
				typ, err = ResolveComponent(fileScope, expr)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveComponent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && typ != nil {
				if tt.isTypeComponent {
					// For type components, verify it's a Named type
					_, ok := typ.(*types.Named)
					if !ok {
						t.Errorf("expected *types.Named for type component, got %T", typ)
					}
					return
				}

				// Check if it's a function signature
				sig, ok := typ.(*types.Signature)
				if !ok {
					// Could be a type component, skip parameter checking
					return
				}

				// Check parameter names
				params := sig.Params()
				if params.Len() != len(tt.wantParams) {
					t.Errorf("got %d params, want %d", params.Len(), len(tt.wantParams))
					return
				}
				for i, wantName := range tt.wantParams {
					if params.At(i).Name() != wantName {
						t.Errorf("param[%d] = %s, want %s", i, params.At(i).Name(), wantName)
					}
				}
			}
		})
	}
}

func TestSymbolResolverV2_ResolveExpression(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		scope    string // code to set up scope
		wantType string
		wantErr  bool
	}{
		{
			name:     "simple variable",
			expr:     "name",
			scope:    `var name string`,
			wantType: "string",
			wantErr:  false,
		},
		{
			name:     "struct field",
			expr:     "user.Email",
			scope:    `type User struct { Email string }; var user User`,
			wantType: "string",
			wantErr:  false,
		},
		{
			name:     "slice index",
			expr:     "items[0]",
			scope:    `var items []string`,
			wantType: "string",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)

			// Create a test Go file with the scope setup
			goContent := `package test

` + tt.scope + `

func testFunc() {
	_ = ` + tt.expr + `
}`

			// Parse the Go file
			file, err := parser.ParseFile(env.fset, "test.go", goContent, 0)
			if err != nil {
				t.Fatalf("failed to parse Go file: %v", err)
			}

			// Type check to get scope
			conf := types.Config{
				Importer: nil, // We don't need imports for these tests
			}
			info := &types.Info{
				Scopes: make(map[ast.Node]*types.Scope),
			}
			pkg, err := conf.Check("test", env.fset, []*ast.File{file}, info)
			if err != nil {
				t.Fatalf("type check failed: %v", err)
			}

			// Parse the expression
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("failed to parse expression: %v", err)
			}

			// Resolve using package scope
			typ, err := ResolveExpression(expr, pkg.Scope())
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveExpression() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && typ != nil {
				gotType := typ.String()
				if gotType != tt.wantType {
					t.Errorf("got type %s, want %s", gotType, tt.wantType)
				}
			}
		})
	}
}

// Helper to create a test environment
type testEnv struct {
	dir      string
	resolver *SymbolResolverV2
	fset     *token.FileSet
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()

	// Create a go.mod file for the test environment
	// Get the absolute path to the templ module root
	templRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("failed to get templ root path: %v", err)
	}

	// Create a go.mod file with proper pseudo-version for replace directive
	// The pseudo-version format ensures Go recognizes this as a valid version
	goModContent := `module testmod

go 1.23

require github.com/a-h/templ v0.0.0-00010101000000-000000000000

replace github.com/a-h/templ => ` + templRoot + `
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goModContent), 0o644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	// Create a simple Go file that imports github.com/a-h/templ to ensure it's not removed by go mod tidy
	importerContent := `package main

import _ "github.com/a-h/templ"
`
	if err := os.WriteFile(filepath.Join(dir, "importer.go"), []byte(importerContent), 0o644); err != nil {
		t.Fatalf("failed to create importer.go: %v", err)
	}

	// Run go mod tidy to resolve all dependencies
	// This is necessary because packages.Load needs all transitive dependencies
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to run go mod tidy: %v\nOutput: %s", err, output)
	}

	return &testEnv{
		dir:      dir,
		resolver: NewSymbolResolverV2(),
		fset:     token.NewFileSet(),
	}
}

func (e *testEnv) writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(e.dir, name)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	return path
}

func (e *testEnv) parseTemplFile(t *testing.T, content string) *templparser.TemplateFile {
	t.Helper()
	// Write to temp file and parse
	templFile := e.writeFile(t, "test.templ", content)
	tf, err := templparser.Parse(templFile)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}
	return tf
}

// Test overlay generation
func TestSymbolResolverV2_GenerateOverlay(t *testing.T) {
	tests := []struct {
		name         string
		templContent string
		wantContains []string // strings that should appear in overlay
	}{
		{
			name: "simple component",
			templContent: `package main

templ Hello(name string) {
	<div>Hello { name }</div>
}`,
			wantContains: []string{
				"package main",
				"func Hello(name string) templ.Component",
				"templ.NopComponent",
			},
		},
		{
			name: "component with imports",
			templContent: `package main

import (
	"fmt"
	"github.com/a-h/templ"
)

templ Greeting(user User) {
	<div>{ fmt.Sprintf("Hello %s", user.Name) }</div>
}`,
			wantContains: []string{
				"import (",
				"fmt",
				"github.com/a-h/templ",
				"func Greeting(user User) templ.Component",
			},
		},
		{
			name: "component with CSS",
			templContent: `package main

css buttonStyle() {
	background-color: blue;
}

templ Button() {
	<button class={ buttonStyle() }>Click</button>
}`,
			wantContains: []string{
				"func buttonStyle() templ.CSSClass",
				"templ.ComponentCSSClass{}",
				"func Button() templ.Component",
			},
		},
		{
			name: "CSS with parameters",
			templContent: `package main

import "fmt"

css loading(percent int) {
	width: { fmt.Sprintf("%d%%", percent) };
}

templ ProgressBar(progress int) {
	<div class={ loading(progress) }>Loading...</div>
}`,
			wantContains: []string{
				"import",
				"fmt",
				"func loading(percent int) templ.CSSClass",
				"templ.ComponentCSSClass{}",
				"func ProgressBar(progress int) templ.Component",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			tf := env.parseTemplFile(t, tt.templContent)

			overlay, err := env.resolver.generateOverlay(tf)
			if err != nil {
				t.Fatalf("GenerateOverlay failed: %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(overlay, want) {
					t.Errorf("overlay missing %q\nGot:\n%s", want, overlay)
				}
			}
		})
	}
}
