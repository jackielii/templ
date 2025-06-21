package parser

import (
	"strings"
	"testing"

	"github.com/a-h/parse"
	"github.com/google/go-cmp/cmp"
)

func TestGoCodeParser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *GoCode
	}{
		{
			name:  "basic expression",
			input: `{{ p := "this" }}`,
			expected: &GoCode{
				Expression: Expression{
					Value: `p := "this"`,
					Range: Range{
						From: Position{
							Index: 3,
							Line:  0,
							Col:   3,
						},
						To: Position{
							Index: 14,
							Line:  0,
							Col:   14,
						},
					},
				},
			},
		},
		{
			name:  "basic expression, no space",
			input: `{{p:="this"}}`,
			expected: &GoCode{
				Expression: Expression{
					Value: `p:="this"`,
					Range: Range{
						From: Position{
							Index: 2,
							Line:  0,
							Col:   2,
						},
						To: Position{
							Index: 11,
							Line:  0,
							Col:   11,
						},
					},
				},
			},
		},
		{
			name: "multiline function decl",
			input: `{{
				p := func() {
					dosomething()
				}
			}}`,
			expected: &GoCode{
				Expression: Expression{
					Value: `
				p := func() {
					dosomething()
				}`,
					Range: Range{
						From: Position{
							Index: 2,
							Line:  0,
							Col:   2,
						},
						To: Position{
							Index: 45,
							Line:  3,
							Col:   5,
						},
					},
				},
				Multiline: true,
			},
		},
		{
			name: "comments in expression",
			input: `{{
	one := "one"
	two := "two"
	// Comment in middle of expression.
	four := "four"
	// Comment at end of expression.
}}`,
			expected: &GoCode{
				Expression: Expression{
					Value: `
	one := "one"
	two := "two"
	// Comment in middle of expression.
	four := "four"
	// Comment at end of expression.`,
					Range: Range{
						From: Position{Index: 2, Line: 0, Col: 2},
						To:   Position{Index: 117, Line: 5, Col: 33},
					},
				},
				TrailingSpace: SpaceNone,
				Multiline:     true,
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			input := parse.NewInput(tt.input)
			an, ok, err := goCode.Parse(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ok {
				t.Fatalf("unexpected failure for input %q", tt.input)
			}
			actual := an.(*GoCode)
			if diff := cmp.Diff(tt.expected, actual); diff != "" {
				t.Error(diff)
			}

			// Check the index.
			cut := tt.input[actual.Expression.Range.From.Index:actual.Expression.Range.To.Index]
			if tt.expected.Expression.Value != cut {
				t.Errorf("range, expected %q, got %q", tt.expected.Expression.Value, cut)
			}
		})
	}
}

func TestGoImportParser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *TemplateFileGoExpression
	}{
		{
			name:  "single import",
			input: `import "fmt"`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`import "fmt"`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 12, Line: 1, Col: 13}),
			},
		},
		{
			name:  "aliased import",
			input: `import f "fmt"`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`import f "fmt"`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 14, Line: 1, Col: 15}),
			},
		},
		{
			name:     "not an import",
			input:    `const x = 5`,
			expected: nil,
		},
		{
			name: "multi-line import block",
			input: `import (
	"fmt"
	"time"
)`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`import (
	"fmt"
	"time"
)`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 32, Line: 4, Col: 2}),
			},
		},
		{
			name: "import block with comments",
			input: `import (
	"fmt" // formatting
	"time" // time operations
)`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`import (
	"fmt" // formatting
	"time" // time operations
)`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 58, Line: 4, Col: 2}),
			},
		},
		{
			name: "import block with aliases",
			input: `import (
	f "fmt"
	_ "embed"
)`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`import (
	f "fmt"
	_ "embed"
)`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 31, Line: 4, Col: 2}),
			},
		},
		{
			name:     "empty import block",
			input:    `import ()`,
			expected: nil, // Empty import blocks are handled by default Go parser
		},
		{
			name:  "import with dot",
			input: `import . "fmt"`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`import . "fmt"`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 14, Line: 1, Col: 15}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi := parse.NewInput(tt.input)
			got, ok, err := goImportParser.Parse(pi)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expected == nil {
				if ok {
					t.Errorf("expected ok=false, got true with result: %+v", got)
				}
				return
			}

			if !ok {
				t.Errorf("expected ok=true, got false")
				return
			}

			// Compare the expression value
			if got.Expression.Value != tt.expected.Expression.Value {
				t.Errorf("expression value mismatch:\ngot:  %q\nwant: %q", got.Expression.Value, tt.expected.Expression.Value)
			}

			// The Stmt field should be populated for valid imports
			if got.Expression.Stmt == nil {
				t.Errorf("expected Stmt to be populated, got nil")
			}
		})
	}
}

func TestGoImportParserIntegration(t *testing.T) {
	t.Run("single import", func(t *testing.T) {
		input := `package main

import "fmt"

templ Hello() {
	<div>{ fmt.Sprintf("Hello") }</div>
}`

		tf, err := ParseString(input)
		if err != nil {
			t.Fatalf("failed to parse template: %v", err)
		}

		// Should have 2 nodes: import and templ
		if len(tf.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(tf.Nodes))
		}

		// First node should be the import
		importNode, ok := tf.Nodes[0].(*TemplateFileGoExpression)
		if !ok {
			t.Errorf("expected first node to be TemplateFileGoExpression, got %T", tf.Nodes[0])
		} else {
			if !strings.Contains(importNode.Expression.Value, `import "fmt"`) {
				t.Errorf("expected import expression to contain 'import \"fmt\"', got %q", importNode.Expression.Value)
			}
		}

		// Second node should be the template
		_, ok = tf.Nodes[1].(*HTMLTemplate)
		if !ok {
			t.Errorf("expected second node to be HTMLTemplate, got %T", tf.Nodes[1])
		}
	})

	t.Run("multi-line import block", func(t *testing.T) {
		input := `package main

import (
	"fmt"
	"time"
)

templ Hello() {
	<div>{ fmt.Sprintf("Hello at %v", time.Now()) }</div>
}`

		tf, err := ParseString(input)
		if err != nil {
			t.Fatalf("failed to parse template: %v", err)
		}

		// Should have 2 nodes: import block and templ
		if len(tf.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(tf.Nodes))
		}

		// First node should be the import block
		importNode, ok := tf.Nodes[0].(*TemplateFileGoExpression)
		if !ok {
			t.Errorf("expected first node to be TemplateFileGoExpression, got %T", tf.Nodes[0])
		} else {
			if !strings.Contains(importNode.Expression.Value, `import (`) {
				t.Errorf("expected import block to start with 'import (', got %q", importNode.Expression.Value)
			}
			if !strings.Contains(importNode.Expression.Value, `"fmt"`) {
				t.Errorf("expected import block to contain '\"fmt\"', got %q", importNode.Expression.Value)
			}
			if !strings.Contains(importNode.Expression.Value, `"time"`) {
				t.Errorf("expected import block to contain '\"time\"', got %q", importNode.Expression.Value)
			}
		}

		// Second node should be the template
		_, ok = tf.Nodes[1].(*HTMLTemplate)
		if !ok {
			t.Errorf("expected second node to be HTMLTemplate, got %T", tf.Nodes[1])
		}
	})
}

func TestGoTopLevelCommentParser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *TemplateFileGoExpression
	}{
		{
			name:  "single line comment",
			input: `// This is a comment`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`// This is a comment`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 20, Line: 1, Col: 21}),
			},
		},
		{
			name: "multi-line comment",
			input: `/* This is a
multi-line comment */`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`/* This is a
multi-line comment */`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 35, Line: 2, Col: 22}),
			},
		},
		{
			name:     "not a comment",
			input:    `import "fmt"`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi := parse.NewInput(tt.input)
			got, ok, err := goCommentParser.Parse(pi)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expected == nil {
				if ok {
					t.Errorf("expected ok=false, got true with result: %+v", got)
				}
				return
			}

			if !ok {
				t.Errorf("expected ok=true, got false")
				return
			}

			// Compare the expression value
			if got.Expression.Value != tt.expected.Expression.Value {
				t.Errorf("expression value mismatch:\ngot:  %q\nwant: %q", got.Expression.Value, tt.expected.Expression.Value)
			}
		})
	}
}

func TestGoConstDeclParser(t *testing.T) {
	t.Skip("Const parser not implemented yet - using default Go parser")
}

func TestGoTypeDeclParser(t *testing.T) {
	t.Skip("Type parser not implemented yet - using default Go parser")
}

func TestGoVarDeclParser(t *testing.T) {
	t.Skip("Var parser not implemented yet - using default Go parser")
}

func TestGoDeclParsersIntegration(t *testing.T) {
	t.Run("single imports", func(t *testing.T) {
		input := `package main

import "fmt"
import "time"

const DefaultTimeout = 30

type Config struct {
	Name string
}

var logger = GetLogger()

templ Hello() {
	<div>{ fmt.Sprintf("Hello at %v", time.Now()) }</div>
}`

		tf, err := ParseString(input)
		if err != nil {
			t.Fatalf("failed to parse template: %v", err)
		}

		// Count different types of nodes
		var imports, goExpressions, templates int
		for _, node := range tf.Nodes {
			switch n := node.(type) {
			case *TemplateFileGoExpression:
				if strings.HasPrefix(n.Expression.Value, "import") {
					imports++
				} else {
					goExpressions++
				}
			case *HTMLTemplate:
				templates++
			}
		}

		if imports != 2 {
			t.Errorf("expected 2 imports, got %d", imports)
		}
		if goExpressions == 0 {
			t.Errorf("expected some Go expressions for const/type/var, got %d", goExpressions)
		}
		if templates != 1 {
			t.Errorf("expected 1 template, got %d", templates)
		}
	})

	t.Run("import block", func(t *testing.T) {
		input := `package main

import (
	"fmt"
	"time"
	"strings"
)

const DefaultTimeout = 30

templ Hello() {
	<div>{ fmt.Sprintf("Hello at %v", time.Now()) }</div>
}`

		tf, err := ParseString(input)
		if err != nil {
			t.Fatalf("failed to parse template: %v", err)
		}

		// Count different types of nodes
		var imports, goExpressions, templates int
		for _, node := range tf.Nodes {
			switch n := node.(type) {
			case *TemplateFileGoExpression:
				if strings.HasPrefix(n.Expression.Value, "import") {
					imports++
				} else {
					goExpressions++
				}
			case *HTMLTemplate:
				templates++
			}
		}

		// Should only have 1 import (the whole block)
		if imports != 1 {
			t.Errorf("expected 1 import block, got %d", imports)
		}
		if goExpressions == 0 {
			t.Errorf("expected some Go expressions for const/type/var, got %d", goExpressions)
		}
		if templates != 1 {
			t.Errorf("expected 1 template, got %d", templates)
		}
	})

	t.Run("mixed import block and single imports", func(t *testing.T) {
		input := `package main

import "context"

import (
	"fmt"
	"time"
)

import "strings"

const DefaultTimeout = 30

templ Hello() {
	<div>{ fmt.Sprintf("Hello at %v", time.Now()) }</div>
}`

		tf, err := ParseString(input)
		if err != nil {
			t.Fatalf("failed to parse template: %v", err)
		}

		// Count different types of nodes
		var imports, goExpressions, templates int
		var importValues []string
		for _, node := range tf.Nodes {
			switch n := node.(type) {
			case *TemplateFileGoExpression:
				if strings.HasPrefix(n.Expression.Value, "import") {
					imports++
					importValues = append(importValues, n.Expression.Value)
				} else {
					goExpressions++
				}
			case *HTMLTemplate:
				templates++
			}
		}

		// Should have 3 imports: single "context", block with "fmt" and "time", single "strings"
		if imports != 3 {
			t.Errorf("expected 3 imports, got %d", imports)
			for i, v := range importValues {
				t.Logf("Import %d: %q", i+1, v)
			}
		}

		// Verify the order and content
		if len(importValues) >= 3 {
			if !strings.Contains(importValues[0], `import "context"`) {
				t.Errorf("expected first import to be 'import \"context\"', got %q", importValues[0])
			}
			if !strings.Contains(importValues[1], "import (") || !strings.Contains(importValues[1], `"fmt"`) || !strings.Contains(importValues[1], `"time"`) {
				t.Errorf("expected second import to be an import block with fmt and time, got %q", importValues[1])
			}
			if !strings.Contains(importValues[2], `import "strings"`) {
				t.Errorf("expected third import to be 'import \"strings\"', got %q", importValues[2])
			}
		}

		if goExpressions == 0 {
			t.Errorf("expected some Go expressions for const/type/var, got %d", goExpressions)
		}
		if templates != 1 {
			t.Errorf("expected 1 template, got %d", templates)
		}
	})

	t.Run("comprehensive import test", func(t *testing.T) {
		// This tests various import styles all in one file with comments
		input := `package main

// Standard library imports
import "fmt"
import "time"

// Third-party imports with aliases
import (
	"github.com/a-h/templ"
	g "github.com/gorilla/mux"
	_ "github.com/lib/pq" // postgres driver
)

// Dot import for testing
import . "github.com/stretchr/testify/assert"

const DefaultTimeout = 30

templ Hello() {
	<div>Hello</div>
}`

		tf, err := ParseString(input)
		if err != nil {
			t.Fatalf("failed to parse template: %v", err)
		}

		// Count and collect imports
		var imports []string
		var comments, consts, templates int

		for _, node := range tf.Nodes {
			switch n := node.(type) {
			case *TemplateFileGoExpression:
				val := strings.TrimSpace(n.Expression.Value)
				if strings.HasPrefix(val, "import") {
					imports = append(imports, val)
				} else if strings.HasPrefix(val, "//") {
					comments++
				} else if strings.HasPrefix(val, "const") {
					consts++
				}
			case *HTMLTemplate:
				templates++
			}
		}

		// Should have 4 imports: 2 single imports, 1 import block, 1 dot import
		if len(imports) != 4 {
			t.Errorf("expected 4 imports, got %d", len(imports))
			for i, imp := range imports {
				t.Logf("Import %d: %q", i+1, imp)
			}
		}

		// Verify we got the expected imports
		expectedPatterns := []string{
			`import "fmt"`,
			`import "time"`,
			`import (`, // The import block
			`import . "github.com/stretchr/testify/assert"`,
		}

		for i, pattern := range expectedPatterns {
			if i >= len(imports) {
				t.Errorf("missing import %d: expected to contain %q", i+1, pattern)
				continue
			}
			if !strings.Contains(imports[i], pattern) {
				t.Errorf("import %d: expected to contain %q, got %q", i+1, pattern, imports[i])
			}
		}

		// Should have 3 comments
		if comments != 3 {
			t.Errorf("expected 3 comments, got %d", comments)
		}

		if consts != 1 {
			t.Errorf("expected 1 const, got %d", consts)
		}

		if templates != 1 {
			t.Errorf("expected 1 template, got %d", templates)
		}
	})
}
