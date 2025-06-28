package symbolresolver

import (
	"strings"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

func TestOverlayGenerationWithLocalVariables(t *testing.T) {
	tests := []struct {
		name           string
		templ          string
		expectedInCode []string
	}{
		{
			name: "simple go code block",
			templ: `package test

import "fmt"

templ ShowUser(user User) {
	{{ myVar := "hello" }}
	<div>{ myVar }</div>
}`,
			expectedInCode: []string{
				"package test",
				"\"fmt\"",
				"\"github.com/a-h/templ\"",
				"func ShowUser(user User) templ.Component {",
				"// Local variables from template",
				"var myVar interface{}",
				"_ = myVar",
				"return templ.NopComponent",
			},
		},
		{
			name: "for loop with range",
			templ: `package test

templ ShowItems(items []Item) {
	for i, item := range items {
		<div>{ item.Name }</div>
	}
}`,
			expectedInCode: []string{
				"func ShowItems(items []Item) templ.Component {",
				"// Local variables from template",
				"var i int",
				"_ = i",
				"var item interface{}",
				"_ = item",
			},
		},
		{
			name: "nested scopes",
			templ: `package test

templ Complex(data Data) {
	{{ outer := "outer value" }}
	<div>
		for _, item := range data.Items {
			{{ inner := item.Process() }}
			<span>{ inner }</span>
		}
	</div>
}`,
			expectedInCode: []string{
				"func Complex(data Data) templ.Component {",
				"// Local variables from template",
				"var outer interface{}",
				"_ = outer",
				"var item interface{}",
				"_ = item",
				"var inner interface{}",
				"_ = inner",
			},
		},
		{
			name: "no local variables",
			templ: `package test

templ Simple(name string) {
	<div>Hello { name }</div>
}`,
			expectedInCode: []string{
				"func Simple(name string) templ.Component {",
				"return templ.NopComponent",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the template
			tf, err := parser.ParseString(tt.templ)
			if err != nil {
				t.Fatalf("Failed to parse template: %v", err)
			}

			// Generate overlay
			resolver := NewSymbolResolverV2()
			overlay, err := resolver.generateOverlay(tf)
			if err != nil {
				t.Fatalf("Failed to generate overlay: %v", err)
			}

			// Check if expected code is present
			for _, expected := range tt.expectedInCode {
				if !strings.Contains(overlay, expected) {
					t.Errorf("Expected overlay to contain %q, but it didn't.\nOverlay:\n%s", expected, overlay)
				}
			}
		})
	}
}