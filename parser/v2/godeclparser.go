package parser

import (
	"strings"

	"github.com/a-h/parse"
	"github.com/a-h/templ/parser/v2/goexpression"
)

// https://go.dev/ref/spec#Declarations_and_scope
// Declaration  = ConstDecl | TypeDecl | VarDecl .
// TopLevelDecl = Declaration | FunctionDecl | MethodDecl .

// TODO: use goexpression/parse.go to parse this into an ast.Node
var goCommentParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	start := pi.Position()

	// Check if this line starts with a comment
	if !peekPrefix(pi, "//", "/*") {
		return
	}

	// For single-line comments
	if peekPrefix(pi, "//") {
		// Read the comment line
		var line string
		line, ok, err = stringUntilNewLineOrEOF.Parse(pi)
		if err != nil || !ok {
			return
		}

		// Don't include the newline in the expression to make endsWithComment work correctly
		end := pi.Position()
		expr := NewExpression(line, start, end)

		// But still consume the newline from input
		parse.NewLine.Parse(pi)

		n = &TemplateFileGoExpression{
			Expression: expr,
		}

		return n, true, nil
	}

	// For multi-line comments
	if peekPrefix(pi, "/*") {
		// Read until we find */
		var comment strings.Builder
		for {
			peek, ok := pi.Peek(2)
			if !ok {
				break
			}

			if peek == "*/" {
				pi.Take(2)
				comment.WriteString("*/")
				break
			}

			// Take one character at a time
			pi.Take(1)
			comment.WriteByte(peek[0])
		}

		// Don't include trailing newline in the expression
		end := pi.Position()
		expr := NewExpression(comment.String(), start, end)

		// But still consume the newline from input
		parse.NewLine.Parse(pi)

		n = &TemplateFileGoExpression{
			Expression: expr,
		}

		return n, true, nil
	}

	return nil, false, nil
})

// getGoGenDeclParser returns a parser for Go declaration expressions like import, func, const, and type.
func getGoGenDeclParser(keywords ...string) parse.Parser[*TemplateFileGoExpression] {
	return parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
		if !peekPrefix(pi, keywords...) {
			return
		}

		expr, err := parseGo(strings.Join(keywords, ","), pi, goexpression.GenDecl)
		if err != nil {
			return nil, false, err
		}

		// Check for trailing semicolon and consume it if present
		consumeTrailingSemicolon(pi, &expr)

		n = &TemplateFileGoExpression{
			Expression: expr,
		}

		return n, true, nil
	})
}

// goFuncDeclParser parses func and method declarations in Go.
var goFuncDeclParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	if !peekPrefix(pi, "func ", "func\t", "func(", "func (") {
		return
	}

	expr, err := parseGo("func", pi, goexpression.Func)
	if err != nil {
		return nil, false, err
	}

	n = &TemplateFileGoExpression{
		Expression: expr,
	}

	return n, true, nil
})

var (
	goImportParser    = getGoGenDeclParser("import ", "import\t", "import(", "import (", "import\t(")
	goConstDeclParser = getGoGenDeclParser("const ", "const\t", "const(", "const (", "const\t(")
	goTypeDeclParser  = getGoGenDeclParser("type ", "type\t", "type(", "type (", "type\t(")
	goVarDeclParser   = getGoGenDeclParser("var ", "var\t", "var(", "var (", "var\t(")
)

// consumeTrailingSemicolon checks for a trailing semicolon and consumes it if present,
// updating the expression to include it. It also consumes any whitespace before the semicolon.
func consumeTrailingSemicolon(pi *parse.Input, expr *Expression) {
	// Store the current position in case we need to backtrack
	startPos := pi.Index()

	// Try to parse optional whitespace
	space, matched, _ := parse.Whitespace.Parse(pi)

	// Check if the next character is a semicolon
	if peek, ok := pi.Peek(1); ok && peek == ";" {
		pi.Take(1)
		// Update the expression to include the whitespace and semicolon
		if matched {
			expr.Value = expr.Value + space + ";"
		} else {
			expr.Value = expr.Value + ";"
		}
		pos := pi.Position()
		expr.Range.To = Position{
			Index: int64(pos.Index),
			Line:  uint32(pos.Line),
			Col:   uint32(pos.Col),
		}
	} else if matched {
		// If we found whitespace but no semicolon, backtrack
		pi.Seek(startPos)
	}
}
