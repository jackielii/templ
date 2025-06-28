package symbolresolver

import (
	"testing"

	"github.com/a-h/templ/parser/v2"
)

func TestVariableExtractor(t *testing.T) {
	tests := []struct {
		name     string
		templ    string
		expected map[string]string
	}{
		{
			name: "simple go code block",
			templ: `package test

templ ShowUser(user User) {
	{{ myVar := "hello" }}
	<div>{ myVar }</div>
}`,
			expected: map[string]string{
				"user":  "User",
				"myVar": "interface{}",
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
			expected: map[string]string{
				"items": "[]Item",
				"i":     "int",
				"item":  "interface{}",
			},
		},
		{
			name: "if statement with assignment",
			templ: `package test

templ Process() {
	if err := doSomething(); err != nil {
		<div>Error: { err.Error() }</div>
	}
}`,
			expected: map[string]string{
				"err": "interface{}",
			},
		},
		{
			name: "switch with type assertion",
			templ: `package test

templ ShowValue(v interface{}) {
	switch val := v.(type) {
	case string:
		<div>String: { val }</div>
	case int:
		<div>Int: { val }</div>
	}
}`,
			expected: map[string]string{
				"v":   "interface{}",
				"val": "interface{}", // Type switches are complex to handle perfectly
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
			expected: map[string]string{
				"data":  "Data",
				"outer": "interface{}",
				"item":  "interface{}",
				"inner": "interface{}",
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

			// Find the HTML template
			var htmlTemplate *parser.HTMLTemplate
			for _, node := range tf.Nodes {
				if tmpl, ok := node.(*parser.HTMLTemplate); ok {
					htmlTemplate = tmpl
					break
				}
			}

			if htmlTemplate == nil {
				t.Fatal("No HTML template found")
			}

			// Extract variables
			extractor := newTemplateVariableExtractor()
			vars := extractor.extractFromHTMLTemplate(htmlTemplate)

			
			// Check if all expected variables are found
			for varName, expectedType := range tt.expected {
				actualType, found := vars[varName]
				if !found {
					t.Errorf("Expected variable %s not found", varName)
					continue
				}
				if actualType != expectedType {
					t.Errorf("Variable %s: expected type %s, got %s", varName, expectedType, actualType)
				}
			}

			// Check for unexpected variables
			for varName := range vars {
				if _, expected := tt.expected[varName]; !expected {
					t.Errorf("Unexpected variable found: %s", varName)
				}
			}
		})
	}
}

func TestExtractTemplateParameters(t *testing.T) {
	tests := []struct {
		signature string
		expected  map[string]string
	}{
		{
			signature: "ShowUser(user User, enabled bool)",
			expected: map[string]string{
				"user":    "User",
				"enabled": "bool",
			},
		},
		{
			signature: "ShowItems(items []Item)",
			expected: map[string]string{
				"items": "[]Item",
			},
		},
		{
			signature: "ShowMap(data map[string]interface{})",
			expected: map[string]string{
				"data": "map[string]interface{}",
			},
		},
		{
			signature: "NoParams()",
			expected:  map[string]string{},
		},
		{
			signature: "Variadic(prefix string, values ...string)",
			expected: map[string]string{
				"prefix": "string",
				"values": "[]string",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.signature, func(t *testing.T) {
			extractor := newTemplateVariableExtractor()
			extractor.extractTemplateParameters(tt.signature)

			for varName, expectedType := range tt.expected {
				actualType, found := extractor.currentScope.variables[varName]
				if !found {
					t.Errorf("Expected parameter %s not found", varName)
					continue
				}
				if actualType != expectedType {
					t.Errorf("Parameter %s: expected type %s, got %s", varName, expectedType, actualType)
				}
			}
		})
	}
}