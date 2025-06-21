package parser

import (
	"strings"

	"github.com/a-h/parse"
	"github.com/a-h/templ/parser/v2/goexpression"
)

// goCode is the parser used to parse Raw Go code within templates.
//
// goCodeInJavaScript is the same, but handles the case where Go expressions
// are embedded within JavaScript.
//
// The only difference is that goCode normalises whitespace after the
// closing brace pair, whereas goCodeInJavaScript retains all whitespace.
var (
	goCode             = getGoCodeParser(true)
	goCodeInJavaScript = getGoCodeParser(false)
)

func getGoCodeParser(normalizeWhitespace bool) parse.Parser[Node] {
	return parse.Func(func(pi *parse.Input) (n Node, ok bool, err error) {
		// Check the prefix first.
		if _, ok, err = dblOpenBraceWithOptionalPadding.Parse(pi); err != nil || !ok {
			return
		}

		// Once we have a prefix, we must have an expression that returns a string, with optional err.
		l := pi.Position().Line
		r := &GoCode{}
		if r.Expression, err = parseGo("go code", pi, goexpression.Expression); err != nil {
			return r, false, err
		}

		if l != pi.Position().Line {
			r.Multiline = true
		}

		// Clear any optional whitespace.
		_, _, _ = parse.OptionalWhitespace.Parse(pi)

		// }}
		if _, ok, err = dblCloseBraceWithOptionalPadding.Parse(pi); err != nil || !ok {
			err = parse.Error("go code: missing close braces", pi.Position())
			return
		}

		// Parse trailing whitespace.
		ws, _, err := parse.Whitespace.Parse(pi)
		if err != nil {
			return r, false, err
		}
		if normalizeWhitespace {
			if r.TrailingSpace, err = NewTrailingSpace(ws); err != nil {
				return r, false, err
			}
		} else {
			r.TrailingSpace = TrailingSpace(ws)
		}

		return r, true, nil
	})
}

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
	_, _, stmt, parseErr := goexpression.Import(content)
	if parseErr != nil {
		// If parsing fails, reset and let default parser handle it
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

// https://go.dev/ref/spec#Declarations_and_scope
// Declaration  = ConstDecl | TypeDecl | VarDecl .
// TopLevelDecl = Declaration | FunctionDecl | MethodDecl .

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
	// For now, let the default Go parser handle func declarations
	return nil, false, nil
})

var goConstDeclParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	// For now, let the default Go parser handle const declarations
	return nil, false, nil
})

var goTypeDeclParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	// For now, let the default Go parser handle type declarations
	return nil, false, nil
})

var goVarDeclParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	// For now, let the default Go parser handle var declarations
	return nil, false, nil
})

var goMethodDeclParser = parse.Func(func(pi *parse.Input) (n *TemplateFileGoExpression, ok bool, err error) {
	// For now, let the default Go parser handle method declarations
	return nil, false, nil
})
