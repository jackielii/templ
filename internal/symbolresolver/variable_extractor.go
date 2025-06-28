package symbolresolver

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	v2parser "github.com/a-h/templ/parser/v2"
)

// variableScope represents a local scope within a template
type variableScope struct {
	parent     *variableScope
	variables  map[string]string // variable name -> type expression
	blockType  string            // "template", "if", "for", "switch", "gocode"
	expression string            // the original expression for if/for/switch
}

// newVariableScope creates a new variable scope
func newVariableScope(parent *variableScope, blockType string) *variableScope {
	return &variableScope{
		parent:    parent,
		variables: make(map[string]string),
		blockType: blockType,
	}
}

// lookup searches for a variable in this scope and its parents
func (vs *variableScope) lookup(name string) (string, bool) {
	if typ, ok := vs.variables[name]; ok {
		return typ, true
	}
	if vs.parent != nil {
		return vs.parent.lookup(name)
	}
	return "", false
}

// templateVariableExtractor extracts variables from template nodes
type templateVariableExtractor struct {
	currentScope *variableScope
	allScopes    []*variableScope
	errors       []error
}

// newTemplateVariableExtractor creates a new extractor
func newTemplateVariableExtractor() *templateVariableExtractor {
	rootScope := newVariableScope(nil, "template")
	return &templateVariableExtractor{
		currentScope: rootScope,
		allScopes:    []*variableScope{rootScope},
		errors:       []error{},
	}
}

// extractFromHTMLTemplate extracts variables from an HTML template
func (e *templateVariableExtractor) extractFromHTMLTemplate(tmpl *v2parser.HTMLTemplate) map[string]string {
	// Extract parameters
	e.extractTemplateParameters(tmpl.Expression.Value)
	
	// Process child nodes
	e.extractFromNodes(tmpl.Children)
	
	// Return all variables found from all scopes
	result := make(map[string]string)
	for _, scope := range e.allScopes {
		for name, typ := range scope.variables {
			result[name] = typ
		}
	}
	return result
}

// extractTemplateParameters extracts parameter variables from a template signature
func (e *templateVariableExtractor) extractTemplateParameters(signature string) {
	// Parse the signature to extract parameters
	// Example: "ShowUser(user User, enabled bool)"
	// We need to extract: user -> User, enabled -> bool
	
	openParen := strings.Index(signature, "(")
	closeParen := strings.LastIndex(signature, ")")
	
	if openParen == -1 || closeParen == -1 || closeParen <= openParen {
		return
	}
	
	params := signature[openParen+1 : closeParen]
	if strings.TrimSpace(params) == "" {
		return
	}
	
	// Simple parameter parsing - this handles most common cases
	// For complex cases, we'd need to use go/parser
	paramList := strings.Split(params, ",")
	for _, param := range paramList {
		param = strings.TrimSpace(param)
		if param == "" {
			continue
		}
		
		// Find the last space to split name and type
		lastSpace := strings.LastIndex(param, " ")
		if lastSpace == -1 {
			continue
		}
		
		varName := strings.TrimSpace(param[:lastSpace])
		varType := strings.TrimSpace(param[lastSpace+1:])
		
		// Handle variadic parameters like "values ...string"
		if strings.HasPrefix(varType, "...") {
			varType = "[]" + varType[3:]
		}
		
		e.currentScope.variables[varName] = varType
	}
}

// extractFromNodes processes a list of nodes
func (e *templateVariableExtractor) extractFromNodes(nodes []v2parser.Node) {
	for _, node := range nodes {
		e.extractFromNode(node)
	}
}

// extractFromNode processes a single node
func (e *templateVariableExtractor) extractFromNode(node v2parser.Node) {
	switch n := node.(type) {
	case *v2parser.GoCode:
		e.extractFromGoCode(n)
	case *v2parser.IfExpression:
		e.extractFromIfExpression(n)
	case *v2parser.ForExpression:
		e.extractFromForExpression(n)
	case *v2parser.SwitchExpression:
		e.extractFromSwitchExpression(n)
	case *v2parser.Element:
		// Process child nodes
		e.extractFromNodes(n.Children)
	case *v2parser.TemplElementExpression:
		// Process child nodes
		e.extractFromNodes(n.Children)
	case *v2parser.ElementComponent:
		// Process child nodes
		e.extractFromNodes(n.Children)
	}
}

// extractFromGoCode extracts variables from a {{ ... }} block
func (e *templateVariableExtractor) extractFromGoCode(gc *v2parser.GoCode) {
	// fmt.Printf("DEBUG: extractFromGoCode called with: %s\n", gc.Expression.Value)
	
	// Create a new scope for this block
	scope := newVariableScope(e.currentScope, "gocode")
	e.allScopes = append(e.allScopes, scope)
	oldScope := e.currentScope
	e.currentScope = scope
	defer func() { e.currentScope = oldScope }()
	
	// Parse the Go code to find variable declarations
	e.extractVariablesFromGoExpression(gc.Expression.Value)
}

// extractFromIfExpression extracts variables from if expressions
func (e *templateVariableExtractor) extractFromIfExpression(ie *v2parser.IfExpression) {
	// Create a new scope for the if block
	scope := newVariableScope(e.currentScope, "if")
	scope.expression = ie.Expression.Value
	e.allScopes = append(e.allScopes, scope)
	oldScope := e.currentScope
	e.currentScope = scope
	
	// Extract any variables from the condition (e.g., if err := doSomething(); err != nil)
	e.extractVariablesFromGoExpression(ie.Expression.Value)
	
	// Process then branch
	e.extractFromNodes(ie.Then)
	
	// Process else-if branches
	for _, elseIf := range ie.ElseIfs {
		// Reset to parent scope for else-if condition
		e.currentScope = oldScope
		elseIfScope := newVariableScope(e.currentScope, "if")
		elseIfScope.expression = elseIf.Expression.Value
		e.allScopes = append(e.allScopes, elseIfScope)
		e.currentScope = elseIfScope
		
		e.extractVariablesFromGoExpression(elseIf.Expression.Value)
		e.extractFromNodes(elseIf.Then)
	}
	
	// Process else branch
	if len(ie.Else) > 0 {
		e.currentScope = oldScope
		elseScope := newVariableScope(e.currentScope, "else")
		e.allScopes = append(e.allScopes, elseScope)
		e.currentScope = elseScope
		e.extractFromNodes(ie.Else)
	}
	
	e.currentScope = oldScope
}

// extractFromForExpression extracts variables from for loops
func (e *templateVariableExtractor) extractFromForExpression(fe *v2parser.ForExpression) {
	// Create a new scope for the for block
	scope := newVariableScope(e.currentScope, "for")
	scope.expression = fe.Expression.Value
	e.allScopes = append(e.allScopes, scope)
	oldScope := e.currentScope
	e.currentScope = scope
	defer func() { e.currentScope = oldScope }()
	
	// Extract loop variables (e.g., for i, v := range items)
	e.extractVariablesFromForExpression(fe.Expression.Value)
	
	// Process child nodes
	e.extractFromNodes(fe.Children)
}

// extractFromSwitchExpression extracts variables from switch statements
func (e *templateVariableExtractor) extractFromSwitchExpression(se *v2parser.SwitchExpression) {
	// Create a new scope for the switch block
	scope := newVariableScope(e.currentScope, "switch")
	scope.expression = se.Expression.Value
	e.allScopes = append(e.allScopes, scope)
	oldScope := e.currentScope
	e.currentScope = scope
	defer func() { e.currentScope = oldScope }()
	
	// Extract any variables from the switch expression
	e.extractVariablesFromGoExpression(se.Expression.Value)
	
	// Process cases
	for _, c := range se.Cases {
		// Each case has its own scope
		caseScope := newVariableScope(e.currentScope, "case")
		caseScope.expression = c.Expression.Value
		e.allScopes = append(e.allScopes, caseScope)
		caseScopeOld := e.currentScope
		e.currentScope = caseScope
		
		// Extract variables from case expression if it's a type switch
		if strings.Contains(se.Expression.Value, ".(type)") {
			e.extractVariablesFromCaseExpression(c.Expression.Value)
		}
		
		e.extractFromNodes(c.Children)
		e.currentScope = caseScopeOld
	}
}

// extractVariablesFromGoExpression extracts variable declarations from Go code
func (e *templateVariableExtractor) extractVariablesFromGoExpression(expr string) {
	// Try to parse as a statement
	src := "package p\nfunc _() { " + expr + " }"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		// Not a valid statement, might be an expression
		return
	}
	
	if len(f.Decls) == 0 {
		return
	}
	
	funcDecl, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok || funcDecl.Body == nil || len(funcDecl.Body.List) == 0 {
		return
	}
	
	// Extract variables from statements
	for _, stmt := range funcDecl.Body.List {
		e.extractVariablesFromStatement(stmt)
	}
}

// extractVariablesFromStatement extracts variables from a Go statement
func (e *templateVariableExtractor) extractVariablesFromStatement(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.DeclStmt:
		if genDecl, ok := s.Decl.(*ast.GenDecl); ok && genDecl.Tok == token.VAR {
			for _, spec := range genDecl.Specs {
				if valueSpec, ok := spec.(*ast.ValueSpec); ok {
					e.extractVariablesFromValueSpec(valueSpec)
				}
			}
		}
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE { // :=
			e.extractVariablesFromAssignment(s)
		}
	case *ast.IfStmt:
		// Handle if init statements
		if s.Init != nil {
			e.extractVariablesFromStatement(s.Init)
		}
	}
}

// extractVariablesFromValueSpec extracts variables from a var declaration
func (e *templateVariableExtractor) extractVariablesFromValueSpec(spec *ast.ValueSpec) {
	var typeStr string
	if spec.Type != nil {
		typeStr = exprToString(spec.Type)
	} else if len(spec.Values) > 0 {
		// Try to infer type from value
		typeStr = "interface{}"
	}
	
	for _, name := range spec.Names {
		if name.Name != "_" {
			e.currentScope.variables[name.Name] = typeStr
		}
	}
}

// extractVariablesFromAssignment extracts variables from := assignments
func (e *templateVariableExtractor) extractVariablesFromAssignment(assign *ast.AssignStmt) {
	for _, lhs := range assign.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok && ident.Name != "_" {
			// For now, use interface{} as the type
			// A full implementation would infer types from RHS
			e.currentScope.variables[ident.Name] = "interface{}"
		}
	}
}

// extractVariablesFromForExpression extracts loop variables
func (e *templateVariableExtractor) extractVariablesFromForExpression(expr string) {
	// Parse the for expression
	src := "package p\nfunc _() { for " + expr + " {} }"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		e.errors = append(e.errors, fmt.Errorf("failed to parse for expression: %w", err))
		return
	}
	
	if len(f.Decls) == 0 {
		return
	}
	
	funcDecl, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok || funcDecl.Body == nil || len(funcDecl.Body.List) == 0 {
		return
	}
	
	stmt := funcDecl.Body.List[0]
	
	// Check if it's a range statement
	if rangeStmt, ok := stmt.(*ast.RangeStmt); ok {
		// Extract key and value variables
		if rangeStmt.Key != nil {
			if ident, ok := rangeStmt.Key.(*ast.Ident); ok && ident.Name != "_" {
				e.currentScope.variables[ident.Name] = "int" // Default to int for index
			}
		}
		if rangeStmt.Value != nil {
			if ident, ok := rangeStmt.Value.(*ast.Ident); ok && ident.Name != "_" {
				// For now, use interface{} as the type
				e.currentScope.variables[ident.Name] = "interface{}"
			}
		}
	} else if forStmt, ok := stmt.(*ast.ForStmt); ok && forStmt.Init != nil {
		// Handle regular for loop init
		e.extractVariablesFromStatement(forStmt.Init)
	}
}

// extractVariablesFromCaseExpression extracts variables from type switch cases
func (e *templateVariableExtractor) extractVariablesFromCaseExpression(expr string) {
	// Handle type switch cases like "case v := x.(Type):"
	if strings.Contains(expr, ":=") {
		parts := strings.Split(expr, ":=")
		if len(parts) == 2 {
			varName := strings.TrimSpace(parts[0])
			varName = strings.TrimPrefix(varName, "case ")
			varName = strings.TrimSpace(varName)
			
			// Extract type from the assertion
			typePart := strings.TrimSpace(parts[1])
			if strings.Contains(typePart, ".(") && strings.Contains(typePart, ")") {
				start := strings.Index(typePart, ".(") + 2
				end := strings.Index(typePart, ")")
				if end > start {
					typeStr := typePart[start:end]
					e.currentScope.variables[varName] = typeStr
				}
			}
		}
	}
}

// collectAllVariablesRecursive collects all variables from a scope and its children
func (e *templateVariableExtractor) collectAllVariablesRecursive(scope *variableScope, result map[string]string) {
	if scope == nil {
		return
	}
	
	// First collect from parent scope
	if scope.parent != nil {
		e.collectAllVariablesRecursive(scope.parent, result)
	}
	
	// Then add variables from this scope (overwrites parent if shadowed)
	for name, typ := range scope.variables {
		result[name] = typ
	}
}

// exprToString converts an AST expression to a string representation
func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + exprToString(e.Elt)
		}
		return "[" + exprToString(e.Len) + "]" + exprToString(e.Elt)
	case *ast.MapType:
		return "map[" + exprToString(e.Key) + "]" + exprToString(e.Value)
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return "interface{}"
	}
}