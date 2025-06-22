package parser

import ()

import (
	"go/ast"
	"go/token"
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
			name:  "empty import block",
			input: `import ()`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`import ()`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 9, Line: 1, Col: 10}),
			},
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
	tests := []struct {
		name     string
		input    string
		expected *TemplateFileGoExpression
	}{
		{
			name:  "single const",
			input: `const DefaultTimeout = 30`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`const DefaultTimeout = 30`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 25, Line: 1, Col: 26}),
			},
		},
		{
			name:  "const with semicolon",
			input: `const DefaultTimeout = 30;`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`const DefaultTimeout = 30;`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 26, Line: 1, Col: 27}),
			},
		},
		{
			name:  "const with type",
			input: `const DefaultTimeout time.Duration = 30 * time.Second`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`const DefaultTimeout time.Duration = 30 * time.Second`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 54, Line: 1, Col: 55}),
			},
		},
		{
			name: "const block",
			input: `const (
	DefaultTimeout = 30
	MaxRetries = 3
)`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`const (
	DefaultTimeout = 30
	MaxRetries = 3
)`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 49, Line: 4, Col: 2}),
			},
		},
		{
			name:     "not a const",
			input:    `var x = 5`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi := parse.NewInput(tt.input)
			got, ok, err := goConstDeclParser.Parse(pi)
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

func TestGoTypeDeclParser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *TemplateFileGoExpression
	}{
		{
			name:  "type alias",
			input: `type ID string`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`type ID string`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 14, Line: 1, Col: 15}),
			},
		},
		{
			name:  "type with space and semicolon",
			input: `type ID string ;`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`type ID string ;`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 16, Line: 1, Col: 17}),
			},
		},
		{
			name:  "single line struct",
			input: `type Point struct { X, Y int }`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`type Point struct { X, Y int }`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 30, Line: 1, Col: 31}),
			},
		},
		{
			name: "multi-line struct",
			input: `type Config struct {
	Name string
	Timeout int
}`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`type Config struct {
	Name string
	Timeout int
}`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 47, Line: 4, Col: 2}),
			},
		},
		{
			name: "interface",
			input: `type Writer interface {
	Write([]byte) (int, error)
}`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`type Writer interface {
	Write([]byte) (int, error)
}`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 52, Line: 3, Col: 2}),
			},
		},
		{
			name:     "not a type",
			input:    `const x = 5`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi := parse.NewInput(tt.input)
			got, ok, err := goTypeDeclParser.Parse(pi)
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

func TestGoVarDeclParser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *TemplateFileGoExpression
	}{
		{
			name:  "single var",
			input: `var logger = GetLogger()`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`var logger = GetLogger()`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 24, Line: 1, Col: 25}),
			},
		},
		{
			name:  "var with semicolon",
			input: `var a = false;`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`var a = false;`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 14, Line: 1, Col: 15}),
			},
		},
		{
			name:  "var with space before semicolon",
			input: `var a = false ;`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`var a = false ;`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 15, Line: 1, Col: 16}),
			},
		},
		{
			name:  "var with type",
			input: `var count int = 42`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`var count int = 42`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 18, Line: 1, Col: 19}),
			},
		},
		{
			name: "var block",
			input: `var (
	x int
	y = "hello"
)`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`var (
	x int
	y = "hello"
)`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 29, Line: 4, Col: 2}),
			},
		},
		{
			name:     "not a var",
			input:    `const x = 5`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi := parse.NewInput(tt.input)
			got, ok, err := goVarDeclParser.Parse(pi)
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

func TestAllGoDeclParsersIntegration(t *testing.T) {
	input := `package main

// Package documentation
import "fmt"
import "time"

import (
	"context"
	"errors"
)

// Constants
const DefaultTimeout = 30
const (
	MaxRetries = 3
	MinRetries = 1
)

// Types
type ID string

type Config struct {
	Name    string
	Timeout int
}

type Logger interface {
	Log(message string)
}

// Variables
var logger = GetLogger()
var (
	config *Config
	debug  = false
)

// Functions
func main() {
	fmt.Println("Hello")
}

func GetLogger() Logger {
	return &defaultLogger{}
}

// Methods
func (c *Config) Validate() error {
	if c.Name == "" {
		return errors.New("name required")
	}
	return nil
}

func (l *defaultLogger) Log(message string) {
	fmt.Println(message)
}

templ Hello() {
	<div>Hello World</div>
}`

	tf, err := ParseString(input)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Count different types of nodes
	var comments, imports, consts, types, vars, funcs, methods, templates int
	for _, node := range tf.Nodes {
		switch n := node.(type) {
		case *TemplateFileGoExpression:
			val := strings.TrimSpace(n.Expression.Value)
			switch {
			case strings.HasPrefix(val, "//") || strings.HasPrefix(val, "/*"):
				comments++
			case strings.HasPrefix(val, "import"):
				imports++
			case strings.HasPrefix(val, "const"):
				consts++
			case strings.HasPrefix(val, "type"):
				types++
			case strings.HasPrefix(val, "var"):
				vars++
			case strings.HasPrefix(val, "func ("):
				methods++
			case strings.HasPrefix(val, "func"):
				funcs++
			}
		case *HTMLTemplate:
			templates++
		}
	}

	// Verify counts
	if comments != 6 { // Package doc, Constants, Types, Variables, Functions, Methods
		t.Errorf("expected 6 comments, got %d", comments)
	}
	if imports != 3 { // 2 single imports + 1 import block
		t.Errorf("expected 3 imports, got %d", imports)
	}
	if consts != 2 { // 1 single const + 1 const block
		t.Errorf("expected 2 consts, got %d", consts)
	}
	if types != 3 { // ID, Config, Logger
		t.Errorf("expected 3 types, got %d", types)
	}
	if vars != 2 { // 1 single var + 1 var block
		t.Errorf("expected 2 vars, got %d", vars)
	}
	if funcs != 2 { // main, GetLogger
		t.Errorf("expected 2 funcs, got %d", funcs)
	}
	if methods != 2 { // Validate, Log
		t.Errorf("expected 2 methods, got %d", methods)
	}
	if templates != 1 { // Hello
		t.Errorf("expected 1 template, got %d", templates)
	}
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

func TestGoFuncDeclParser(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *TemplateFileGoExpression
	}{
		{
			name:  "simple function",
			input: `func main() {}`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`func main() {}`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 14, Line: 1, Col: 15}),
			},
		},
		{
			name: "function with body",
			input: `func Hello() string {
	return "Hello"
}`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`func Hello() string {
	return "Hello"
}`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 38, Line: 3, Col: 2}),
			},
		},
		{
			name: "function with parameters",
			input: `func Add(a, b int) int {
	return a + b
}`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`func Add(a, b int) int {
	return a + b
}`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 40, Line: 3, Col: 2}),
			},
		},
		{
			name:     "not a func",
			input:    `var x = 5`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi := parse.NewInput(tt.input)
			got, ok, err := goFuncDeclParser.Parse(pi)
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

func TestGoFuncDeclParserWithMethods(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *TemplateFileGoExpression
	}{
		{
			name:  "simple method",
			input: `func (p *Person) Name() string { return p.name }`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`func (p *Person) Name() string { return p.name }`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 49, Line: 1, Col: 50}),
			},
		},
		{
			name: "method with body",
			input: `func (c *Config) Validate() error {
	if c.Name == "" {
		return errors.New("name required")
	}
	return nil
}`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`func (c *Config) Validate() error {
	if c.Name == "" {
		return errors.New("name required")
	}
	return nil
}`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 98, Line: 6, Col: 2}),
			},
		},
		{
			name:  "function is also parsed by unified parser",
			input: `func main() {}`,
			expected: &TemplateFileGoExpression{
				Expression: NewExpression(`func main() {}`, parse.Position{Index: 0, Line: 1, Col: 1}, parse.Position{Index: 14, Line: 1, Col: 15}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi := parse.NewInput(tt.input)
			got, ok, err := goFuncDeclParser.Parse(pi)
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

func TestGoDeclParsersWithAST(t *testing.T) {
	t.Run("const AST", func(t *testing.T) {
		input := `const DefaultTimeout = 30`
		pi := parse.NewInput(input)
		got, ok, err := goConstDeclParser.Parse(pi)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("expected ok=true, got false")
		}

		// Check that AST is populated
		if got.Expression.Stmt == nil {
			t.Fatalf("expected Stmt to be populated, got nil")
		}

		// Verify it's a GenDecl with CONST token
		genDecl, ok := got.Expression.Stmt.(*ast.GenDecl)
		if !ok {
			t.Fatalf("expected *ast.GenDecl, got %T", got.Expression.Stmt)
		}
		if genDecl.Tok != token.CONST {
			t.Errorf("expected CONST token, got %v", genDecl.Tok)
		}
	})

	t.Run("type AST", func(t *testing.T) {
		input := `type Config struct { Name string }`
		pi := parse.NewInput(input)
		got, ok, err := goTypeDeclParser.Parse(pi)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("expected ok=true, got false")
		}

		// Check that AST is populated
		if got.Expression.Stmt == nil {
			t.Fatalf("expected Stmt to be populated, got nil")
		}

		// Verify it's a GenDecl with TYPE token
		genDecl, ok := got.Expression.Stmt.(*ast.GenDecl)
		if !ok {
			t.Fatalf("expected *ast.GenDecl, got %T", got.Expression.Stmt)
		}
		if genDecl.Tok != token.TYPE {
			t.Errorf("expected TYPE token, got %v", genDecl.Tok)
		}
	})

	t.Run("var AST", func(t *testing.T) {
		input := `var logger = GetLogger()`
		pi := parse.NewInput(input)
		got, ok, err := goVarDeclParser.Parse(pi)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("expected ok=true, got false")
		}

		// Check that AST is populated
		if got.Expression.Stmt == nil {
			t.Fatalf("expected Stmt to be populated, got nil")
		}

		// Verify it's a GenDecl with VAR token
		genDecl, ok := got.Expression.Stmt.(*ast.GenDecl)
		if !ok {
			t.Fatalf("expected *ast.GenDecl, got %T", got.Expression.Stmt)
		}
		if genDecl.Tok != token.VAR {
			t.Errorf("expected VAR token, got %v", genDecl.Tok)
		}
	})

	t.Run("func AST", func(t *testing.T) {
		input := `func main() { fmt.Println("Hello") }`
		pi := parse.NewInput(input)
		got, ok, err := goFuncDeclParser.Parse(pi)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("expected ok=true, got false")
		}

		// Check that AST is populated
		if got.Expression.Stmt == nil {
			t.Fatalf("expected Stmt to be populated, got nil")
		}

		// Verify it's a FuncDecl
		funcDecl, ok := got.Expression.Stmt.(*ast.FuncDecl)
		if !ok {
			t.Fatalf("expected *ast.FuncDecl, got %T", got.Expression.Stmt)
		}
		if funcDecl.Name.Name != "main" {
			t.Errorf("expected function name 'main', got %q", funcDecl.Name.Name)
		}
		if funcDecl.Recv != nil {
			t.Errorf("expected no receiver for function, got %v", funcDecl.Recv)
		}
	})

	t.Run("method AST", func(t *testing.T) {
		input := `func (c *Config) Validate() error { return nil }`
		pi := parse.NewInput(input)
		got, ok, err := goFuncDeclParser.Parse(pi)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("expected ok=true, got false")
		}

		// Check that AST is populated
		if got.Expression.Stmt == nil {
			t.Fatalf("expected Stmt to be populated, got nil")
		}

		// Verify it's a FuncDecl with receiver
		funcDecl, ok := got.Expression.Stmt.(*ast.FuncDecl)
		if !ok {
			t.Fatalf("expected *ast.FuncDecl, got %T", got.Expression.Stmt)
		}
		if funcDecl.Name.Name != "Validate" {
			t.Errorf("expected method name 'Validate', got %q", funcDecl.Name.Name)
		}
		if funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 {
			t.Errorf("expected receiver for method, got none")
		}
	})
}
