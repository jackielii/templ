package parser

import (
	"strings"

	"github.com/a-h/parse"
	"github.com/a-h/templ/parser/v2/goexpression"
)

// https://go.dev/ref/spec#Declarations_and_scope
// Declaration  = ConstDecl | TypeDecl | VarDecl .
// TopLevelDecl = Declaration | FunctionDecl | MethodDecl .

var goImportParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	start := pi.Position()

	if !peekPrefix(pi, "import ", "import\t", "import(", "import (") {
		return
	}

	// Read the first line to check if it's a single-line or multi-line import
	var firstLine string
	firstLine, ok, err = stringUntilNewLineOrEOF.Parse(pi)
	if err != nil || !ok {
		return
	}

	// Reset position
	pi.Seek(start.Index)

	var content string
	// Check if it's a multi-line import block
	if strings.Contains(firstLine, "(") {
		var importContent strings.Builder
		// Multi-line import block - read until closing parenthesis
		parenDepth := 0
		for {
			peek, ok := pi.Peek(1)
			if !ok {
				break
			}

			char := peek[0]
			pi.Take(1)
			importContent.WriteByte(char)

			if char == '(' {
				parenDepth++
			} else if char == ')' {
				parenDepth--
				if parenDepth == 0 {
					break
				}
			}
		}
		content = importContent.String()
	} else {
		// Single-line import - just read the line
		content, _, _ = stringUntilNewLineOrEOF.Parse(pi)
	}

	// Parse the import to get the AST (validation)
	_, _, stmt, err := goexpression.Import(content)
	if err != nil {
		pi.Seek(start.Index)
		return nil, false, nil
	}

	// Create expression
	end := pi.Position()
	expr := NewExpression(content, start, end)
	expr.Stmt = stmt

	n = &TemplateFileGoExpression{
		Expression: expr,
	}

	return n, true, nil
})

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

var goFuncDeclParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	start := pi.Position()

	if !peekPrefix(pi, "func ", "func\t", "func(", "func (") {
		return
	}

	expr, err := parseGo("func", pi, goexpression.Func)
	if err != nil {
		pi.Seek(start.Index)
		return nil, false, nil
	}

	n = &TemplateFileGoExpression{
		Expression: expr,
	}

	return n, true, nil
})

var goConstDeclParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	start := pi.Position()

	if !peekPrefix(pi, "const ", "const\t", "const(", "const (") {
		return
	}

	// Use parseGo with the standard approach
	expr, err := parseGo("const", pi, goexpression.Const)
	if err != nil {
		pi.Seek(start.Index)
		return nil, false, nil
	}

	// Check for trailing semicolon and consume it if present
	consumeTrailingSemicolon(pi, &expr)

	n = &TemplateFileGoExpression{
		Expression: expr,
	}

	return n, true, nil
})

var goTypeDeclParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	start := pi.Position()

	if !peekPrefix(pi, "type ", "type\t") {
		return
	}

	expr, err := parseGo("type", pi, goexpression.Type)
	if err != nil {
		pi.Seek(start.Index)
		return nil, false, nil
	}

	// Check for trailing semicolon and consume it if present
	consumeTrailingSemicolon(pi, &expr)

	n = &TemplateFileGoExpression{
		Expression: expr,
	}

	return n, true, nil
})

var goVarDeclParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	start := pi.Position()

	if !peekPrefix(pi, "var ", "var\t", "var(", "var (") {
		return
	}

	// Use parseGo with a custom extractor that includes the "var" keyword
	expr, err := parseGo("var", pi, func(content string) (start, end int, stmt any, err error) {
		_, declEnd, genDecl, err := goexpression.Var(content)
		if err != nil {
			return 0, 0, nil, err
		}
		// Ensure end doesn't exceed content length
		if declEnd > len(content) {
			declEnd = len(content)
		}
		// Include the full declaration from the beginning (including "var")
		return 0, declEnd, genDecl, nil
	})
	if err != nil {
		// Reset and let default parser handle it
		pi.Seek(start.Index)
		return nil, false, nil
	}

	consumeTrailingSemicolon(pi, &expr)

	n = &TemplateFileGoExpression{
		Expression: expr,
	}

	return n, true, nil
})

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
