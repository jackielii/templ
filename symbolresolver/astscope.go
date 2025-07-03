package symbolresolver

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/a-h/templ/parser/v2"
)

// AssignScopes walks through a template file and assigns proper scopes to nodes
// based on the type information from the loaded packages and overlay AST.
// This must be called after PreprocessFiles has loaded all packages.
func (r *SymbolResolverV2) AssignScopes(tf *parser.TemplateFile) error {
	if tf == nil {
		return fmt.Errorf("template file is nil")
	}
	if tf.Filepath == "" {
		return fmt.Errorf("template file has no filepath")
	}

	// Try to get the overlay AST and type information for this template
	overlayAST, typesInfo, overlayErr := r.getOverlayASTAndTypes(tf.Filepath)
	
	// Get the file scope for this template
	fileScope, err := r.GetFileScope(tf.Filepath)
	if err != nil {
		// If we can't get the file scope, create a basic scope for testing
		if overlayErr != nil {
			// No package loading available - create basic scopes for structure
			return r.assignBasicScopes(tf)
		}
		return fmt.Errorf("failed to get file scope: %w", err)
	}

	// Assign file scope to the template file
	tf.SetScope(&parser.Scope{GoScope: fileScope})

	// Walk through all nodes and assign scopes
	if overlayErr != nil {
		// Fallback to basic scope assignment without overlay
		for _, node := range tf.Nodes {
			if err := r.assignScopesToNode(node, fileScope); err != nil {
				return fmt.Errorf("failed to assign scopes to node: %w", err)
			}
		}
	} else {
		// Use overlay-enhanced scope assignment
		for _, node := range tf.Nodes {
			if err := r.assignScopesToNodeWithOverlay(node, fileScope, overlayAST, typesInfo); err != nil {
				return fmt.Errorf("failed to assign scopes to node: %w", err)
			}
		}
	}

	return nil
}

// assignScopesToNode recursively assigns scopes to a node and its children
func (r *SymbolResolverV2) assignScopesToNode(node parser.TemplateFileNode, parentScope *types.Scope) error {
	switch n := node.(type) {
	case *parser.HTMLTemplate:
		// Create a new scope for the template function
		// The template parameters will be added to this scope
		templateScope := types.NewScope(parentScope, token.NoPos, token.NoPos, "template")
		
		// Parse template parameters and add them to scope
		if err := r.addTemplateParametersToScope(n.Expression.Value, templateScope); err != nil {
			return fmt.Errorf("failed to add template parameters to scope: %w", err)
		}
		
		n.SetScope(&parser.Scope{GoScope: templateScope})
		
		// Process children with the template scope
		return r.assignScopesToChildren(n.Children, templateScope)
	
	case *parser.TemplateFileGoExpression:
		// Go expressions at file level don't need special scopes
		return nil
	
	default:
		// Other node types at file level
		return nil
	}
}

// assignScopesToChildren processes child nodes with the given parent scope
func (r *SymbolResolverV2) assignScopesToChildren(nodes []parser.Node, parentScope *types.Scope) error {
	for _, node := range nodes {
		if err := r.assignScopeToNode(node, parentScope); err != nil {
			return err
		}
	}
	return nil
}

// assignScopeToNode assigns scope to a single node based on its type
func (r *SymbolResolverV2) assignScopeToNode(node parser.Node, parentScope *types.Scope) error {
	switch n := node.(type) {
	case *parser.Element:
		// Only component elements need scope for resolution
		if n.IsComponent() {
			n.SetScope(&parser.Scope{GoScope: parentScope})
		}
		// Process children with same scope
		return r.assignScopesToChildren(n.Children, parentScope)
	
	case *parser.IfExpression:
		// Create scopes for then and else branches
		// The condition might introduce new variables (e.g., if x := foo(); x > 0)
		thenScope := types.NewScope(parentScope, token.NoPos, token.NoPos, "if_then")
		elseScope := types.NewScope(parentScope, token.NoPos, token.NoPos, "if_else")
		
		// Parse the condition for any variable declarations
		if err := r.addIfConditionVariables(n.Expression.Value, thenScope); err != nil {
			return fmt.Errorf("failed to parse if condition: %w", err)
		}
		
		n.SetThenScope(&parser.Scope{GoScope: thenScope})
		n.SetElseScope(&parser.Scope{GoScope: elseScope})
		
		// Process then branch
		if err := r.assignScopesToChildren(n.Then, thenScope); err != nil {
			return err
		}
		
		// Process else branch
		if len(n.Else) > 0 {
			if err := r.assignScopesToChildren(n.Else, elseScope); err != nil {
				return err
			}
		}
		
		// Process else-if branches
		for i, elseIf := range n.ElseIfs {
			elseIfScope := types.NewScope(parentScope, token.NoPos, token.NoPos, fmt.Sprintf("elseif_%d", i))
			
			// Parse the else-if condition
			if err := r.addIfConditionVariables(elseIf.Expression.Value, elseIfScope); err != nil {
				return fmt.Errorf("failed to parse else-if condition: %w", err)
			}
			
			elseIf.SetScope(&parser.Scope{GoScope: elseIfScope})
			n.SetElseIfScope(i, &parser.Scope{GoScope: elseIfScope})
			
			if err := r.assignScopesToChildren(elseIf.Then, elseIfScope); err != nil {
				return err
			}
		}
		
		return nil
	
	case *parser.ForExpression:
		// Create scope for the loop body
		forScope := types.NewScope(parentScope, token.NoPos, token.NoPos, "for")
		
		// Parse the for loop header to extract loop variables
		if err := r.addForLoopVariables(n.Expression.Value, forScope); err != nil {
			return fmt.Errorf("failed to parse for loop: %w", err)
		}
		
		n.SetScope(&parser.Scope{GoScope: forScope})
		
		// Process children with the for scope
		return r.assignScopesToChildren(n.Children, forScope)
	
	case *parser.SwitchExpression:
		// Create scope for the switch
		switchScope := types.NewScope(parentScope, token.NoPos, token.NoPos, "switch")
		
		// Parse switch expression for any variable declarations
		if err := r.addSwitchVariables(n.Expression.Value, switchScope); err != nil {
			return fmt.Errorf("failed to parse switch expression: %w", err)
		}
		
		n.SetScope(&parser.Scope{GoScope: switchScope})
		
		// Process each case
		for i, c := range n.Cases {
			caseScope := types.NewScope(switchScope, token.NoPos, token.NoPos, fmt.Sprintf("case_%d", i))
			
			// Parse case expression for any variable bindings
			if err := r.addCaseVariables(c.Expression.Value, caseScope); err != nil {
				return fmt.Errorf("failed to parse case expression: %w", err)
			}
			
			c.SetScope(&parser.Scope{GoScope: caseScope})
			n.SetCaseScope(i, &parser.Scope{GoScope: caseScope})
			
			if err := r.assignScopesToChildren(c.Children, caseScope); err != nil {
				return err
			}
		}
		
		return nil
	
	case parser.CompositeNode:
		// For other composite nodes, recurse with same scope
		return r.assignScopesToChildren(n.ChildNodes(), parentScope)
	
	default:
		// Simple nodes don't need scope assignment
		return nil
	}
}

// addTemplateParametersToScope parses a template function signature and adds parameters to scope
// Example: "MyTemplate(user User, items []string)"
func (r *SymbolResolverV2) addTemplateParametersToScope(signature string, scope *types.Scope) error {
	// This is a simplified implementation
	// In a real implementation, we would parse the AST from the overlay
	// and use the actual type information
	
	// For now, just extract parameter names
	// Skip the function name and find the parameters
	start := strings.Index(signature, "(")
	end := strings.LastIndex(signature, ")")
	if start == -1 || end == -1 || end <= start {
		return nil // No parameters
	}
	
	params := signature[start+1 : end]
	if strings.TrimSpace(params) == "" {
		return nil // Empty parameter list
	}
	
	// Very simplified parameter parsing
	// In reality, we should use the AST from the overlay
	parts := strings.Split(params, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		
		// Extract parameter name (first word)
		fields := strings.Fields(part)
		if len(fields) >= 2 {
			paramName := fields[0]
			// Add to scope with a placeholder type
			// In real implementation, we'd get the actual type from the overlay AST
			scope.Insert(types.NewVar(token.NoPos, nil, paramName, types.Typ[types.UntypedNil]))
		}
	}
	
	return nil
}

// addIfConditionVariables parses an if condition and adds any declared variables to scope
// Example: "if x := foo(); x > 0"
func (r *SymbolResolverV2) addIfConditionVariables(condition string, scope *types.Scope) error {
	// Check for short variable declaration in condition
	if strings.Contains(condition, ":=") {
		// Very simplified - in reality we'd parse the AST
		parts := strings.Split(condition, ";")
		if len(parts) > 1 {
			// Has init statement
			initStmt := strings.TrimSpace(parts[0])
			if strings.Contains(initStmt, ":=") {
				// Extract variable name
				varParts := strings.Split(initStmt, ":=")
				if len(varParts) > 0 {
					varName := strings.TrimSpace(varParts[0])
					// Remove "if" prefix if present
					varName = strings.TrimPrefix(varName, "if")
					varName = strings.TrimSpace(varName)
					if varName != "" {
						scope.Insert(types.NewVar(token.NoPos, nil, varName, types.Typ[types.UntypedNil]))
					}
				}
			}
		}
	}
	
	return nil
}

// addForLoopVariables parses a for loop header and adds loop variables to scope
// Examples: 
// - "for i := 0; i < 10; i++"
// - "for i, v := range items"
// - "for _, item := range items"
func (r *SymbolResolverV2) addForLoopVariables(forExpr string, scope *types.Scope) error {
	forExpr = strings.TrimSpace(forExpr)
	forExpr = strings.TrimPrefix(forExpr, "for")
	forExpr = strings.TrimSpace(forExpr)
	
	if strings.Contains(forExpr, "range") {
		// Range loop
		parts := strings.Split(forExpr, "range")
		if len(parts) > 0 {
			varPart := strings.TrimSpace(parts[0])
			varPart = strings.TrimSuffix(varPart, ":=")
			varPart = strings.TrimSpace(varPart)
			
			// Split by comma for index and value variables
			vars := strings.Split(varPart, ",")
			for _, v := range vars {
				v = strings.TrimSpace(v)
				if v != "" && v != "_" {
					scope.Insert(types.NewVar(token.NoPos, nil, v, types.Typ[types.UntypedNil]))
				}
			}
		}
	} else if strings.Contains(forExpr, ":=") {
		// Traditional for loop with init
		parts := strings.Split(forExpr, ";")
		if len(parts) > 0 {
			initPart := strings.TrimSpace(parts[0])
			if strings.Contains(initPart, ":=") {
				varParts := strings.Split(initPart, ":=")
				if len(varParts) > 0 {
					varName := strings.TrimSpace(varParts[0])
					if varName != "" {
						scope.Insert(types.NewVar(token.NoPos, nil, varName, types.Typ[types.UntypedNil]))
					}
				}
			}
		}
	}
	
	return nil
}

// addSwitchVariables parses a switch expression and adds any declared variables to scope
// Example: "switch x := foo(); x"
func (r *SymbolResolverV2) addSwitchVariables(switchExpr string, scope *types.Scope) error {
	switchExpr = strings.TrimSpace(switchExpr)
	switchExpr = strings.TrimPrefix(switchExpr, "switch")
	switchExpr = strings.TrimSpace(switchExpr)
	
	// Check for init statement
	if strings.Contains(switchExpr, ";") {
		parts := strings.Split(switchExpr, ";")
		if len(parts) > 0 {
			initPart := strings.TrimSpace(parts[0])
			if strings.Contains(initPart, ":=") {
				varParts := strings.Split(initPart, ":=")
				if len(varParts) > 0 {
					varName := strings.TrimSpace(varParts[0])
					if varName != "" {
						scope.Insert(types.NewVar(token.NoPos, nil, varName, types.Typ[types.UntypedNil]))
					}
				}
			}
		}
	}
	
	return nil
}

// getOverlayASTAndTypes retrieves the overlay AST and type information for a template file
func (r *SymbolResolverV2) getOverlayASTAndTypes(templFilePath string) (*ast.File, *types.Info, error) {
	// Get absolute path for the overlay file
	absFilePath, err := filepath.Abs(templFilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get absolute path: %w", err)
	}
	
	overlayPath := strings.TrimSuffix(absFilePath, ".templ") + "_templ.go"
	
	// Get the directory for package lookup
	absDir := filepath.Dir(absFilePath)
	
	pkg, ok := r.packages[absDir]
	if !ok {
		return nil, nil, fmt.Errorf("package in %s not found in preprocessing cache", absDir)
	}
	
	if pkg.TypesInfo == nil {
		return nil, nil, fmt.Errorf("no type information for package in %s", absDir)
	}
	
	// Find the overlay file in the compiled Go files
	for i, file := range pkg.CompiledGoFiles {
		if file == overlayPath && i < len(pkg.Syntax) {
			return pkg.Syntax[i], pkg.TypesInfo, nil
		}
	}
	
	return nil, nil, fmt.Errorf("overlay file %s not found in package", overlayPath)
}

// assignScopesToNodeWithOverlay assigns scopes using overlay AST information
func (r *SymbolResolverV2) assignScopesToNodeWithOverlay(node parser.TemplateFileNode, parentScope *types.Scope, overlayAST *ast.File, typesInfo *types.Info) error {
	switch n := node.(type) {
	case *parser.HTMLTemplate:
		// Find the corresponding function in the overlay AST
		functionScope, err := r.findTemplateFunctionScope(n, overlayAST, typesInfo)
		if err != nil {
			// Fallback to basic scope assignment
			return r.assignScopesToNode(node, parentScope)
		}
		
		n.SetScope(&parser.Scope{GoScope: functionScope})
		
		// Process children with enhanced scope assignment
		return r.assignScopesToChildrenWithOverlay(n.Children, functionScope, overlayAST, typesInfo)
		
	case *parser.TemplateFileGoExpression:
		// Go expressions at file level don't need special scopes
		return nil
		
	default:
		// Other node types at file level - use basic assignment
		return r.assignScopesToNode(node, parentScope)
	}
}

// assignScopesToChildrenWithOverlay processes child nodes with overlay-enhanced scope assignment
func (r *SymbolResolverV2) assignScopesToChildrenWithOverlay(nodes []parser.Node, parentScope *types.Scope, overlayAST *ast.File, typesInfo *types.Info) error {
	for _, node := range nodes {
		if err := r.assignScopeToNodeWithOverlay(node, parentScope, overlayAST, typesInfo); err != nil {
			return err
		}
	}
	return nil
}

// assignScopeToNodeWithOverlay assigns scope to a single node using overlay information
func (r *SymbolResolverV2) assignScopeToNodeWithOverlay(node parser.Node, parentScope *types.Scope, overlayAST *ast.File, typesInfo *types.Info) error {
	switch n := node.(type) {
	case *parser.Element:
		// Only component elements need scope for resolution
		if n.IsComponent() {
			n.SetScope(&parser.Scope{GoScope: parentScope})
		}
		// Process children with same scope
		return r.assignScopesToChildrenWithOverlay(n.Children, parentScope, overlayAST, typesInfo)
		
	case *parser.IfExpression:
		// Try to find corresponding if statement in overlay AST to get precise scopes
		thenScope, elseScope, err := r.findIfStatementScopes(n, overlayAST, typesInfo, parentScope)
		if err != nil {
			// Fallback to basic scope assignment
			return r.assignScopeToNode(node, parentScope)
		}
		
		n.SetThenScope(&parser.Scope{GoScope: thenScope})
		n.SetElseScope(&parser.Scope{GoScope: elseScope})
		
		// Process branches with their respective scopes
		if err := r.assignScopesToChildrenWithOverlay(n.Then, thenScope, overlayAST, typesInfo); err != nil {
			return err
		}
		
		if len(n.Else) > 0 {
			if err := r.assignScopesToChildrenWithOverlay(n.Else, elseScope, overlayAST, typesInfo); err != nil {
				return err
			}
		}
		
		// Process else-if branches
		for i, elseIf := range n.ElseIfs {
			elseIfScope, err := r.findElseIfStatementScope(elseIf, i, overlayAST, typesInfo, parentScope)
			if err != nil {
				// Fallback
				elseIfScope = types.NewScope(parentScope, token.NoPos, token.NoPos, fmt.Sprintf("elseif_%d", i))
			}
			
			elseIf.SetScope(&parser.Scope{GoScope: elseIfScope})
			n.SetElseIfScope(i, &parser.Scope{GoScope: elseIfScope})
			
			if err := r.assignScopesToChildrenWithOverlay(elseIf.Then, elseIfScope, overlayAST, typesInfo); err != nil {
				return err
			}
		}
		
		return nil
		
	case *parser.ForExpression:
		// Try to find corresponding for statement in overlay AST
		forScope, err := r.findForStatementScope(n, overlayAST, typesInfo, parentScope)
		if err != nil {
			// Fallback to basic scope assignment
			return r.assignScopeToNode(node, parentScope)
		}
		
		n.SetScope(&parser.Scope{GoScope: forScope})
		
		// Process children with the for scope
		return r.assignScopesToChildrenWithOverlay(n.Children, forScope, overlayAST, typesInfo)
		
	case *parser.SwitchExpression:
		// Try to find corresponding switch statement in overlay AST
		switchScope, caseScopes, err := r.findSwitchStatementScopes(n, overlayAST, typesInfo, parentScope)
		if err != nil {
			// Fallback to basic scope assignment
			return r.assignScopeToNode(node, parentScope)
		}
		
		n.SetScope(&parser.Scope{GoScope: switchScope})
		
		// Process each case with its scope
		for i, c := range n.Cases {
			var caseScope *types.Scope
			if i < len(caseScopes) {
				caseScope = caseScopes[i]
			} else {
				caseScope = types.NewScope(switchScope, token.NoPos, token.NoPos, fmt.Sprintf("case_%d", i))
			}
			
			c.SetScope(&parser.Scope{GoScope: caseScope})
			n.SetCaseScope(i, &parser.Scope{GoScope: caseScope})
			
			if err := r.assignScopesToChildrenWithOverlay(c.Children, caseScope, overlayAST, typesInfo); err != nil {
				return err
			}
		}
		
		return nil
		
	case parser.CompositeNode:
		// For other composite nodes, recurse with same scope
		return r.assignScopesToChildrenWithOverlay(n.ChildNodes(), parentScope, overlayAST, typesInfo)
		
	default:
		// Simple nodes don't need scope assignment
		return nil
	}
}

// findTemplateFunctionScope finds the scope for a template function in the overlay AST
func (r *SymbolResolverV2) findTemplateFunctionScope(tmpl *parser.HTMLTemplate, overlayAST *ast.File, typesInfo *types.Info) (*types.Scope, error) {
	// Extract the function name from the template signature
	signature := strings.TrimSpace(tmpl.Expression.Value)
	funcName := r.extractFunctionName(signature)
	
	// Find the function declaration in the overlay AST
	for _, decl := range overlayAST.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok && funcDecl.Name != nil && funcDecl.Name.Name == funcName {
			// Get the scope for this function body
			if scope := typesInfo.Scopes[funcDecl.Type]; scope != nil {
				return scope, nil
			}
			// If no scope found for the function type, try the body
			if funcDecl.Body != nil {
				if scope := typesInfo.Scopes[funcDecl.Body]; scope != nil {
					return scope, nil
				}
			}
		}
	}
	
	return nil, fmt.Errorf("function %s not found in overlay AST", funcName)
}

// findIfStatementScopes finds the scopes for if statement branches
func (r *SymbolResolverV2) findIfStatementScopes(ifExpr *parser.IfExpression, overlayAST *ast.File, typesInfo *types.Info, parentScope *types.Scope) (*types.Scope, *types.Scope, error) {
	// This is a simplified implementation
	// In a real implementation, we would walk the overlay AST to find the matching if statement
	// For now, create new scopes as fallback
	thenScope := types.NewScope(parentScope, token.NoPos, token.NoPos, "if_then")
	elseScope := types.NewScope(parentScope, token.NoPos, token.NoPos, "if_else")
	
	// TODO: Implement actual AST walking to find the corresponding if statement
	// and extract its actual scopes from typesInfo.Scopes
	
	return thenScope, elseScope, nil
}

// findElseIfStatementScope finds the scope for an else-if branch
func (r *SymbolResolverV2) findElseIfStatementScope(elseIf parser.ElseIfExpression, index int, overlayAST *ast.File, typesInfo *types.Info, parentScope *types.Scope) (*types.Scope, error) {
	// Simplified implementation - create new scope
	scope := types.NewScope(parentScope, token.NoPos, token.NoPos, fmt.Sprintf("elseif_%d", index))
	return scope, nil
}

// findForStatementScope finds the scope for a for statement
func (r *SymbolResolverV2) findForStatementScope(forExpr *parser.ForExpression, overlayAST *ast.File, typesInfo *types.Info, parentScope *types.Scope) (*types.Scope, error) {
	// Simplified implementation - create new scope and add loop variables
	forScope := types.NewScope(parentScope, token.NoPos, token.NoPos, "for")
	
	// Parse the for loop expression to add variables
	if err := r.addForLoopVariables(forExpr.Expression.Value, forScope); err != nil {
		return nil, fmt.Errorf("failed to parse for loop variables: %w", err)
	}
	
	return forScope, nil
}

// findSwitchStatementScopes finds the scopes for a switch statement and its cases
func (r *SymbolResolverV2) findSwitchStatementScopes(switchExpr *parser.SwitchExpression, overlayAST *ast.File, typesInfo *types.Info, parentScope *types.Scope) (*types.Scope, []*types.Scope, error) {
	// Simplified implementation
	switchScope := types.NewScope(parentScope, token.NoPos, token.NoPos, "switch")
	
	// Create scopes for each case
	var caseScopes []*types.Scope
	for i := range switchExpr.Cases {
		caseScope := types.NewScope(switchScope, token.NoPos, token.NoPos, fmt.Sprintf("case_%d", i))
		caseScopes = append(caseScopes, caseScope)
	}
	
	return switchScope, caseScopes, nil
}

// extractFunctionName extracts the function name from a template signature
func (r *SymbolResolverV2) extractFunctionName(signature string) string {
	// Remove "templ " prefix if present
	signature = strings.TrimPrefix(signature, "templ ")
	signature = strings.TrimSpace(signature)
	
	// Find the opening parenthesis to get just the name
	if idx := strings.Index(signature, "("); idx != -1 {
		return strings.TrimSpace(signature[:idx])
	}
	
	return signature
}

// assignBasicScopes creates basic scope structure when package loading is not available
func (r *SymbolResolverV2) assignBasicScopes(tf *parser.TemplateFile) error {
	// Create a basic package scope
	packageScope := types.NewScope(types.Universe, token.NoPos, token.NoPos, "package")
	
	// Assign package scope to the template file
	tf.SetScope(&parser.Scope{GoScope: packageScope})
	
	// Walk through all nodes and assign basic scopes
	for _, node := range tf.Nodes {
		if err := r.assignScopesToNode(node, packageScope); err != nil {
			return fmt.Errorf("failed to assign basic scopes to node: %w", err)
		}
	}
	
	return nil
}

// addCaseVariables parses a case expression and adds any type assertion variables
// Example: "case x.(string):"
func (r *SymbolResolverV2) addCaseVariables(caseExpr string, scope *types.Scope) error {
	// This would handle type switches where variables are bound
	// For now, we'll leave it as a placeholder
	return nil
}