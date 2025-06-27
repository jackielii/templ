package symbolresolver

import (
	"strings"
	"testing"

	templparser "github.com/a-h/templ/parser/v2"
)

func TestGenerateOverlay_VarConstDeclarations(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     []string // Strings that should be present in the overlay
	}{
		{
			name: "const declaration",
			template: `package test

const foo = "bar"

templ Component() {
	<div>{ foo }</div>
}`,
			want: []string{
				"package test",
				`const foo = "bar"`,
				"func Component() templ.Component {",
			},
		},
		{
			name: "var declaration",
			template: `package test

var count = 42

templ Component() {
	<div>{ fmt.Sprint(count) }</div>
}`,
			want: []string{
				"package test",
				"var count = 42",
				"func Component() templ.Component {",
			},
		},
		{
			name: "multiple const declarations",
			template: `package test

const (
	a = 1
	b = 2
)

templ Component() {
	<div>{ fmt.Sprint(a + b) }</div>
}`,
			want: []string{
				"package test",
				"const (",
				"a = 1",
				"b = 2",
				")",
				"func Component() templ.Component {",
			},
		},
		{
			name: "multiple var declarations",
			template: `package test

var (
	x int
	y string = "hello"
)

templ Component() {
	<div>{ y }</div>
}`,
			want: []string{
				"package test",
				"var (",
				"x int",
				`y string = "hello"`,
				")",
				"func Component() templ.Component {",
			},
		},
		{
			name: "mixed declarations with comments",
			template: `package test

import "fmt"

// This is a comment about constants
const prefix = "MSG: "

// Global variables
var (
	// Counter tracks the number of messages
	counter int
)

type Message struct {
	Text string
}

func helper() string {
	return "helper"
}

templ Component(msg Message) {
	<div>{ prefix + msg.Text }</div>
}`,
			want: []string{
				"package test",
				`"fmt"`, // Just check that fmt is imported, not the exact format
				"// This is a comment about constants",
				`const prefix = "MSG: "`,
				"// Global variables",
				"var (",
				"// Counter tracks the number of messages",
				"counter int",
				")",
				"type Message struct {",
				"Text string",
				"}",
				"func helper() string {",
				"return \"helper\"",
				"}",
				"func Component(msg Message) templ.Component {",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the template
			tf, err := templparser.ParseString(tt.template)
			if err != nil {
				t.Fatalf("Failed to parse template: %v", err)
			}

			// Create resolver and generate overlay
			resolver := NewSymbolResolverV2()
			overlay, err := resolver.generateOverlay(tf)
			if err != nil {
				t.Fatalf("Failed to generate overlay: %v", err)
			}

			// Check that all expected strings are present
			for _, want := range tt.want {
				if !strings.Contains(overlay, want) {
					t.Errorf("Expected overlay to contain %q, but it didn't.\nOverlay:\n%s", want, overlay)
				}
			}
		})
	}
}