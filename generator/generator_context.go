package generator

import (
	"strings"

	"github.com/a-h/templ/parser/v2"
)

// GeneratorContext tracks position in AST during code generation
type GeneratorContext struct {
	CurrentTemplate *parser.HTMLTemplate // Current template we're generating
	ASTPath         []parser.Node        // Path from root to current node
	LocalScopes     []LocalScope         // Stack of local scopes
}

// LocalScope represents a scope created by an AST node
type LocalScope struct {
	Node      parser.Node              // The AST node that created this scope
	Variables map[string]*TypeInfo     // Variables defined in this scope
}

// NewGeneratorContext creates a new generator context
func NewGeneratorContext() *GeneratorContext {
	return &GeneratorContext{
		ASTPath:     []parser.Node{},
		LocalScopes: []LocalScope{},
	}
}

// PushScope creates a new scope
func (ctx *GeneratorContext) PushScope(node parser.Node) {
	scope := LocalScope{
		Node:      node,
		Variables: make(map[string]*TypeInfo),
	}
	ctx.LocalScopes = append(ctx.LocalScopes, scope)
}

// PopScope removes the current scope
func (ctx *GeneratorContext) PopScope() {
	if len(ctx.LocalScopes) > 0 {
		ctx.LocalScopes = ctx.LocalScopes[:len(ctx.LocalScopes)-1]
	}
}

// AddVariable adds a variable to the current scope
func (ctx *GeneratorContext) AddVariable(name string, typeInfo *TypeInfo) {
	if len(ctx.LocalScopes) > 0 {
		currentScope := &ctx.LocalScopes[len(ctx.LocalScopes)-1]
		currentScope.Variables[name] = typeInfo
	}
}

// EnterNode adds a node to the AST path
func (ctx *GeneratorContext) EnterNode(node parser.Node) {
	ctx.ASTPath = append(ctx.ASTPath, node)
}

// ExitNode removes the current node from the AST path
func (ctx *GeneratorContext) ExitNode() {
	if len(ctx.ASTPath) > 0 {
		ctx.ASTPath = ctx.ASTPath[:len(ctx.ASTPath)-1]
	}
}

// SetCurrentTemplate sets the current template being generated
func (ctx *GeneratorContext) SetCurrentTemplate(tmpl *parser.HTMLTemplate) {
	ctx.CurrentTemplate = tmpl
	// When entering a template, we don't push a new scope here
	// The parameters are added separately by the caller
}

// ClearCurrentTemplate clears the current template
func (ctx *GeneratorContext) ClearCurrentTemplate() {
	if ctx.CurrentTemplate != nil {
		ctx.PopScope() // Remove template parameter scope
		ctx.CurrentTemplate = nil
	}
}

// extractForLoopVariables extracts variables from a for expression
// e.g., "for i, item := range items" -> ["i", "item"]
func extractForLoopVariables(expr parser.Expression) map[string]*TypeInfo {
	vars := make(map[string]*TypeInfo)
	
	// Simple parser for common for loop patterns
	// This is a simplified version - real implementation would be more robust
	exprStr := strings.TrimSpace(expr.Value)
	
	// Handle "for i, v := range expr" pattern
	if strings.Contains(exprStr, ":=") && strings.Contains(exprStr, "range") {
		parts := strings.Split(exprStr, ":=")
		if len(parts) >= 2 {
			varPart := strings.TrimSpace(parts[0])
			varNames := strings.Split(varPart, ",")
			
			for i, varName := range varNames {
				varName = strings.TrimSpace(varName)
				if varName != "" && varName != "_" {
					// First variable in range is usually index/key
					if i == 0 {
						vars[varName] = &TypeInfo{
							FullType: "int", // Simplified - could be string for maps
							IsString: false,
						}
					} else {
						// Second variable is the value - type unknown without more context
						vars[varName] = &TypeInfo{
							FullType: "interface{}", // Generic type
						}
					}
				}
			}
		}
	}
	
	// Handle "for i := 0; i < n; i++" pattern
	// The variable is available in the loop scope
	if strings.Contains(exprStr, ";") {
		parts := strings.Split(exprStr, ";")
		if len(parts) > 0 && strings.Contains(parts[0], ":=") {
			initPart := strings.TrimSpace(parts[0])
			assignParts := strings.Split(initPart, ":=")
			if len(assignParts) >= 2 {
				varName := strings.TrimSpace(assignParts[0])
				if varName != "" {
					vars[varName] = &TypeInfo{
						FullType: "int",
					}
				}
			}
		}
	}
	
	return vars
}

// extractIfConditionVariables extracts variables from if condition
// e.g., "if err := doSomething(); err != nil" -> ["err"]
func extractIfConditionVariables(expr parser.Expression) map[string]*TypeInfo {
	vars := make(map[string]*TypeInfo)
	
	exprStr := strings.TrimSpace(expr.Value)
	
	// Handle short variable declaration in if condition
	if strings.Contains(exprStr, ":=") {
		// Split by semicolon in case of "if x := expr; condition"
		parts := strings.Split(exprStr, ";")
		for _, part := range parts {
			if strings.Contains(part, ":=") {
				assignParts := strings.Split(part, ":=")
				if len(assignParts) >= 2 {
					varName := strings.TrimSpace(assignParts[0])
					if varName != "" && varName != "_" {
						// Without type analysis, we can't know the exact type
						vars[varName] = &TypeInfo{
							FullType: "interface{}",
						}
					}
				}
			}
		}
	}
	
	return vars
}

// extractSwitchVariables extracts variables from switch expression
func extractSwitchVariables(expr parser.Expression) map[string]*TypeInfo {
	vars := make(map[string]*TypeInfo)
	
	exprStr := strings.TrimSpace(expr.Value)
	
	// Handle "switch x := expr; x" pattern
	if strings.Contains(exprStr, ":=") && strings.Contains(exprStr, ";") {
		parts := strings.Split(exprStr, ";")
		if len(parts) > 0 && strings.Contains(parts[0], ":=") {
			assignParts := strings.Split(parts[0], ":=")
			if len(assignParts) >= 2 {
				varName := strings.TrimSpace(assignParts[0])
				if varName != "" && varName != "_" {
					vars[varName] = &TypeInfo{
						FullType: "interface{}",
					}
				}
			}
		}
	}
	
	return vars
}