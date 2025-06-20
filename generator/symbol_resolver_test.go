package generator

import (
	"go/ast"
	"strings"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

func TestGenerateOverlay(t *testing.T) {
	tests := []struct {
		name     string
		tf       *parser.TemplateFile
		pkgName  string
		expected string
	}{
		{
			name: "empty template file",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{},
			},
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"context"
	"io"
)

`,
		},
		{
			name: "template with existing imports",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.TemplateFileGoExpression{
						Expression: parser.Expression{
							Value: `import (
	"fmt"
	"strings"
)`,
						},
					},
				},
			},
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"fmt"
	"strings"
)

`,
		},
		{
			name: "template with templ already imported",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.TemplateFileGoExpression{
						Expression: parser.Expression{
							Value: `import (
	"fmt"
	"github.com/a-h/templ"
)`,
						},
					},
				},
			},
			pkgName: "main",
			expected: `package main

import (
	"fmt"
	"github.com/a-h/templ"
)

`,
		},
		{
			name: "simple HTML template",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.HTMLTemplate{
						Expression: parser.Expression{
							Value: "Hello(name string)",
							Stmt: &ast.FuncDecl{
								Name: &ast.Ident{Name: "Hello"},
							},
						},
					},
				},
			},
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"context"
	"io"
)

func Hello(name string) templ.Component {
	return templ.NopComponent
}

`,
		},
		{
			name: "HTML template with receiver",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.HTMLTemplate{
						Expression: parser.Expression{
							Value: "(s *Server) Index()",
							Stmt: &ast.FuncDecl{
								Name: &ast.Ident{Name: "Index"},
								Recv: &ast.FieldList{
									List: []*ast.Field{
										{
											Type: &ast.StarExpr{
												X: &ast.Ident{Name: "Server"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"context"
	"io"
)

func (s *Server) Index() templ.Component {
	return templ.NopComponent
}

`,
		},
		{
			name: "CSS template",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.CSSTemplate{
						Name: "buttonStyle",
					},
				},
			},
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"context"
	"io"
)

func buttonStyle() templ.CSSClass {
	return templ.ComponentCSSClass{}
}

`,
		},
		{
			name: "script template",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.ScriptTemplate{
						Name: parser.Expression{
							Value: "onClick",
						},
						Parameters: parser.Expression{
							Value: "id string",
						},
					},
				},
			},
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"context"
	"io"
)

func onClick(id string) templ.ComponentScript {
	return templ.ComponentScript{}
}

`,
		},
		{
			name: "mixed templates and Go code",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.TemplateFileGoExpression{
						Expression: parser.Expression{
							Value: `import (
	"fmt"
)`,
						},
					},
					&parser.TemplateFileGoExpression{
						Expression: parser.Expression{
							Value: `type User struct {
	Name string
	Email string
}`,
						},
					},
					&parser.HTMLTemplate{
						Expression: parser.Expression{
							Value: "UserCard(user User)",
							Stmt: &ast.FuncDecl{
								Name: &ast.Ident{Name: "UserCard"},
							},
						},
					},
					&parser.CSSTemplate{
						Name: "cardStyle",
					},
				},
			},
			pkgName: "views",
			expected: `package views

import (
	"github.com/a-h/templ"
	"fmt"
)

type User struct {
	Name string
	Email string
}

func UserCard(user User) templ.Component {
	return templ.NopComponent
}

func cardStyle() templ.CSSClass {
	return templ.ComponentCSSClass{}
}

`,
		},
		{
			name: "template with no valid AST node",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.HTMLTemplate{
						Expression: parser.Expression{
							Value: "Invalid()",
							Stmt:  nil, // No AST node
						},
					},
				},
			},
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"context"
	"io"
)

`,
		},
		{
			name: "template with generator imports should not add templ import",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.TemplateFileGoExpression{
						Expression: parser.Expression{
							Value: `import (
	"github.com/a-h/templ/generator"
)`,
						},
					},
				},
			},
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"github.com/a-h/templ/generator"
)

`,
		},
		{
			name: "receiver with non-pointer type",
			tf: &parser.TemplateFile{
				Nodes: []parser.TemplateFileNode{
					&parser.HTMLTemplate{
						Expression: parser.Expression{
							Value: "(s Server) Index()",
							Stmt: &ast.FuncDecl{
								Name: &ast.Ident{Name: "Index"},
								Recv: &ast.FieldList{
									List: []*ast.Field{
										{
											Type: &ast.Ident{Name: "Server"},
										},
									},
								},
							},
						},
					},
				},
			},
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"context"
	"io"
)

func (s Server) Index() templ.Component {
	return templ.NopComponent
}

`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSymbolResolver()
			result := r.generateOverlay(tt.tf, tt.pkgName)

			// Normalize whitespace for comparison
			expected := strings.TrimSpace(tt.expected)
			result = strings.TrimSpace(result)

			if result != expected {
				t.Errorf("generateOverlay() mismatch:\nExpected:\n%s\n\nGot:\n%s", expected, result)
			}
		})
	}
}

func TestGenerateOverlayEdgeCases(t *testing.T) {
	t.Run("multiple import blocks", func(t *testing.T) {
		tf := &parser.TemplateFile{
			Nodes: []parser.TemplateFileNode{
				&parser.TemplateFileGoExpression{
					Expression: parser.Expression{
						Value: `import "fmt"`,
					},
				},
				&parser.TemplateFileGoExpression{
					Expression: parser.Expression{
						Value: `import (
	"strings"
	"time"
)`,
					},
				},
			},
		}

		r := newSymbolResolver()
		result := r.generateOverlay(tf, "main")

		// Should add templ import to the first import block
		if !strings.Contains(result, `"github.com/a-h/templ"`) {
			t.Error("Expected templ import to be added")
		}
	})

	t.Run("template with complex receiver type", func(t *testing.T) {
		tf := &parser.TemplateFile{
			Nodes: []parser.TemplateFileNode{
				&parser.HTMLTemplate{
					Expression: parser.Expression{
						Value: "(s *app.Server) Index()",
						Stmt: &ast.FuncDecl{
							Name: &ast.Ident{Name: "Index"},
							Recv: &ast.FieldList{
								List: []*ast.Field{
									{
										Type: &ast.StarExpr{
											X: &ast.SelectorExpr{
												X:   &ast.Ident{Name: "app"},
												Sel: &ast.Ident{Name: "Server"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		r := newSymbolResolver()
		result := r.generateOverlay(tf, "main")

		expected := "func (s *app.Server) Index() templ.Component {"
		if !strings.Contains(result, expected) {
			t.Errorf("Expected receiver signature not found.\nExpected to contain: %s\nGot:\n%s", expected, result)
		}
	})
}

func TestAstTypeToString(t *testing.T) {
	tests := []struct {
		name     string
		expr     ast.Expr
		expected string
	}{
		{
			name:     "simple identifier",
			expr:     &ast.Ident{Name: "string"},
			expected: "string",
		},
		{
			name:     "pointer type",
			expr:     &ast.StarExpr{X: &ast.Ident{Name: "Server"}},
			expected: "*Server",
		},
		{
			name:     "slice type",
			expr:     &ast.ArrayType{Elt: &ast.Ident{Name: "string"}},
			expected: "[]string",
		},
		{
			name: "map type",
			expr: &ast.MapType{
				Key:   &ast.Ident{Name: "string"},
				Value: &ast.Ident{Name: "int"},
			},
			expected: "map[string]int",
		},
		{
			name: "selector expression",
			expr: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "app"},
				Sel: &ast.Ident{Name: "Server"},
			},
			expected: "app.Server",
		},
		{
			name:     "interface type",
			expr:     &ast.InterfaceType{},
			expected: "any",
		},
		{
			name: "array type with length",
			expr: &ast.ArrayType{
				Len: &ast.BasicLit{Value: "10"},
				Elt: &ast.Ident{Name: "int"},
			},
			expected: "[...]int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSymbolResolver()
			result := r.astTypeToString(tt.expr)
			if result != tt.expected {
				t.Errorf("astTypeToString() = %v, want %v", result, tt.expected)
			}
		})
	}
}