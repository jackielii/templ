package symbolresolver

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/a-h/templ/parser/v2"
)

// generateOverlay creates a Go stub file for a templ template
func generateOverlay(tf *parser.TemplateFile) (string, error) {
	if tf == nil {
		return "", fmt.Errorf("template file is nil")
	}

	// Extract package name
	pkgName := ""
	if tf.Package.Expression.Value != "" {
		pkgName = strings.TrimPrefix(tf.Package.Expression.Value, "package ")
		pkgName = strings.TrimSpace(pkgName)
	}
	if pkgName == "" {
		return "", fmt.Errorf("no package declaration found")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("package %s\n\n", pkgName))

	// Collect imports and generate stubs
	var imports []*ast.GenDecl
	var hasTemplImport bool
	var needsTemplImport bool
	var bodySection strings.Builder

	// Process nodes
	for _, node := range tf.Nodes {
		switch n := node.(type) {
		case *parser.TemplateFileGoExpression:
			// Skip if no ast node (e.g., comments)
			if n.Expression.AstNode == nil {
				continue
			}

			if genDecl, ok := n.Expression.AstNode.(*ast.GenDecl); ok {
				switch genDecl.Tok {
				case token.IMPORT:
					imports = append(imports, genDecl)
					// Check if templ is imported
					for _, spec := range genDecl.Specs {
						if impSpec, ok := spec.(*ast.ImportSpec); ok {
							if impSpec.Path != nil && strings.Trim(impSpec.Path.Value, `"`) == "github.com/a-h/templ" {
								hasTemplImport = true
							}
						}
					}
				case token.TYPE, token.VAR, token.CONST:
					// Include type, var, and const definitions
					bodySection.WriteString(n.Expression.Value)
					bodySection.WriteString("\n\n")
				}
			} else if funcDecl, ok := n.Expression.AstNode.(*ast.FuncDecl); ok {
				// Include function declarations (non-template functions)
				_ = funcDecl // avoid unused variable warning
				bodySection.WriteString(n.Expression.Value)
				bodySection.WriteString("\n\n")
			}

		case *parser.HTMLTemplate:
			needsTemplImport = true
			// Generate function stub
			signature := strings.TrimSpace(n.Expression.Value)
			bodySection.WriteString(fmt.Sprintf("func %s templ.Component {\n", signature))
			bodySection.WriteString("\treturn templ.NopComponent\n")
			bodySection.WriteString("}\n\n")

		case *parser.CSSTemplate:
			needsTemplImport = true
			// CSS templates can have parameters, use the full expression
			signature := strings.TrimSpace(n.Expression.Value)
			bodySection.WriteString(fmt.Sprintf("func %s templ.CSSClass {\n", signature))
			bodySection.WriteString("\treturn templ.ComponentCSSClass{}\n")
			bodySection.WriteString("}\n\n")

		case *parser.ScriptTemplate:
			needsTemplImport = true
			bodySection.WriteString(fmt.Sprintf("func %s(", n.Name.Value))
			if n.Parameters.Value != "" {
				bodySection.WriteString(n.Parameters.Value)
			}
			bodySection.WriteString(") templ.ComponentScript {\n")
			bodySection.WriteString("\treturn templ.ComponentScript{}\n")
			bodySection.WriteString("}\n\n")
		}
	}

	// Write imports
	if needsTemplImport || len(imports) > 0 {
		if len(imports) > 0 {
			// Check if we have multi-line or single imports
			hasMultiLineImport := false
			for _, imp := range imports {
				if imp.Lparen.IsValid() || len(imp.Specs) > 1 {
					hasMultiLineImport = true
					break
				}
			}

			if hasMultiLineImport || (needsTemplImport && !hasTemplImport) {
				// Write as multi-line import
				sb.WriteString("import (\n")

				// Add templ import first if needed
				if needsTemplImport && !hasTemplImport {
					sb.WriteString("\t\"github.com/a-h/templ\"\n")
				}

				// Add all existing imports
				for _, imp := range imports {
					for _, spec := range imp.Specs {
						if impSpec, ok := spec.(*ast.ImportSpec); ok {
							sb.WriteString("\t")
							if impSpec.Name != nil {
								sb.WriteString(impSpec.Name.Name + " ")
							}
							sb.WriteString(impSpec.Path.Value)
							sb.WriteString("\n")
						}
					}
				}
				sb.WriteString(")\n")
			} else {
				// Single import without templ needed
				for _, imp := range imports {
					sb.WriteString("import ")
					if spec := imp.Specs[0].(*ast.ImportSpec); spec != nil {
						if spec.Name != nil {
							sb.WriteString(spec.Name.Name + " ")
						}
						sb.WriteString(spec.Path.Value)
					}
					sb.WriteString("\n")
				}
			}
		} else if needsTemplImport {
			// No imports exist, create new import
			sb.WriteString("import \"github.com/a-h/templ\"\n")
		}
		sb.WriteString("\n")
	}

	// Write body
	sb.WriteString(bodySection.String())

	return sb.String(), nil
}
