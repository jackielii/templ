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
		input    string
		pkgName  string
		expected string
	}{
		{
			name:    "empty template file",
			input:   "",
			pkgName: "main",
			expected: `package main

`,
		},
		{
			name: "template with existing imports",
			input: `package main

import (
	"fmt"
	"strings"
)`,
			pkgName: "main",
			expected: `package main

import (
	"fmt"
	"strings"
)

`,
		},
		{
			name: "template with templ already imported",
			input: `package main

import (
	"fmt"
	"github.com/a-h/templ"
)`,
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
			input: `package main

templ Hello(name string) {
	<div>Hello, { name }!</div>
}`,
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
)

func Hello(name string) templ.Component {
	return templ.NopComponent
}

`,
		},
		{
			name: "HTML template with receiver",
			input: `package main

type Server struct{}

templ (s *Server) Index() {
	<div>Index page</div>
}`,
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
)

type Server struct{}

func (s *Server) Index() templ.Component {
	return templ.NopComponent
}
`,
		},
		{
			name: "CSS template",
			input: `package main

css buttonStyle() {
	background-color: blue;
	color: white;
}`,
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
)

func buttonStyle() templ.CSSClass {
	return templ.ComponentCSSClass{}
}

`,
		},
		{
			name: "script template",
			input: `package main

script onClick(id string) {
	document.getElementById(id).onclick = function() {
		alert("Clicked!");
	}
}`,
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
)

func onClick(id string) templ.ComponentScript {
	return templ.ComponentScript{}
}

`,
		},
		{
			name: "mixed templates and Go code",
			input: `package views

import (
	"fmt"
)

type User struct {
	Name string
	Email string
}

templ UserCard(user User) {
	<div class={ cardStyle() }>
		<h2>{ user.Name }</h2>
		<p>{ user.Email }</p>
	</div>
}

css cardStyle() {
	padding: 20px;
	border: 1px solid #ccc;
}`,
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
			name: "template with generator imports should still add templ import",
			input: `package main

import (
	"github.com/a-h/templ/generator"
)

templ Hello() {
	<div>Hello</div>
}`,
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
	"github.com/a-h/templ/generator"
)

func Hello() templ.Component {
	return templ.NopComponent
}

`,
		},
		{
			name: "receiver with non-pointer type",
			input: `package main

type Server struct{}

templ (s Server) Index() {
	<div>Index</div>
}`,
			pkgName: "main",
			expected: `package main

import (
	"github.com/a-h/templ"
)

type Server struct{}

func (s Server) Index() templ.Component {
	return templ.NopComponent
}

`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tf, err := parser.ParseString(tt.input)
			if err != nil {
				t.Fatalf("failed to parse template: %v", err)
			}

			result := generateOverlay(tf, tt.pkgName)

			// Normalize whitespace for comparison
			expected := strings.TrimSpace(tt.expected)
			result = strings.TrimSpace(result)

			if result != expected {
				t.Errorf("generateOverlay() mismatch:\nExpected:\n%s\n\nGot:\n%s", expected, result)
			}
		})
	}
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
