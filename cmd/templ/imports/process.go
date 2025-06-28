package imports

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/imports"

	"github.com/a-h/templ/generator"
	"github.com/a-h/templ/parser/v2"
)

var internalImports = []string{"github.com/a-h/templ", "github.com/a-h/templ/runtime"}

func convertTemplToGoURI(templURI string) (isTemplFile bool, goURI string) {
	base, fileName := path.Split(templURI)
	if !strings.HasSuffix(fileName, ".templ") {
		return
	}
	return true, base + (strings.TrimSuffix(fileName, ".templ") + "_templ.go")
}

var fset = token.NewFileSet()

func updateImports(name, src string, existingImports []*ast.ImportSpec) (updated []*ast.ImportSpec, err error) {
	// Prepend existing imports to the source so imports.Process knows about them
	importStmts := ""
	if len(existingImports) > 0 {
		importStmts = "import (\n"
		for _, imp := range existingImports {
			if imp.Name != nil {
				importStmts += fmt.Sprintf("\t%s %s\n", imp.Name.Name, imp.Path.Value)
			} else {
				importStmts += fmt.Sprintf("\t%s\n", imp.Path.Value)
			}
		}
		importStmts += ")\n\n"
	}
	
	// Find package declaration and insert imports after it
	lines := strings.Split(src, "\n")
	var pkgIndex int
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "package ") {
			pkgIndex = i
			break
		}
	}
	
	// Reconstruct source with imports
	modifiedSrc := strings.Join(lines[:pkgIndex+1], "\n") + "\n" + importStmts + strings.Join(lines[pkgIndex+1:], "\n")
	
	// Apply auto imports.
	updatedGoCode, err := imports.Process(name, []byte(modifiedSrc), nil)
	if err != nil {
		return updated, fmt.Errorf("failed to process go code %q: %w", src, err)
	}
	// Get updated imports.
	gofile, err := goparser.ParseFile(fset, name, updatedGoCode, goparser.ImportsOnly)
	if err != nil {
		return updated, fmt.Errorf("failed to get imports from updated go code: %w", err)
	}
	for _, imp := range gofile.Imports {
		if !slices.Contains(internalImports, strings.Trim(imp.Path.Value, "\"")) {
			updated = append(updated, imp)
		}
	}
	return updated, nil
}

func Process(t *parser.TemplateFile) (*parser.TemplateFile, error) {
	if t.Filepath == "" {
		return t, nil
	}
	isTemplFile, fileName := convertTemplToGoURI(t.Filepath)
	if !isTemplFile {
		return t, fmt.Errorf("invalid filepath: %s", t.Filepath)
	}

	// Collect all import nodes from the beginning of the file
	var importSpecs []*ast.ImportSpec
	var nonImportNodes []parser.TemplateFileNode
	
	for _, node := range t.Nodes {
		if goExpr, ok := node.(*parser.TemplateFileGoExpression); ok {
			// Check if this is an import node by looking at the AST
			if goExpr.Expression.AstNode != nil {
				if genDecl, ok := goExpr.Expression.AstNode.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
					// Extract ImportSpecs from the GenDecl
					for _, spec := range genDecl.Specs {
						if impSpec, ok := spec.(*ast.ImportSpec); ok {
							importSpecs = append(importSpecs, impSpec)
						}
					}
					continue
				}
			}
		}
		nonImportNodes = append(nonImportNodes, node)
	}

	// Generate code.
	gw := bytes.NewBuffer(nil)
	if _, err := generator.Generate(t, gw); err != nil {
		return t, fmt.Errorf("failed to generate go code: %w", err)
	}
	
	updatedImports, err := updateImports(fileName, gw.String(), importSpecs)
	if err != nil {
		return t, fmt.Errorf("failed to get imports from generated go code: %w", err)
	}
	
	// Debug: log what we found
	// fmt.Printf("DEBUG: Found %d import specs from AST\n", len(importSpecs))
	// fmt.Printf("DEBUG: Found %d updated imports from generated code\n", len(updatedImports))
	
	// Create a minimal AST file to work with astutil
	// We need this because astutil functions require an *ast.File
	// Make a copy of importSpecs because astutil will modify the slice
	fileImports := make([]*ast.ImportSpec, len(importSpecs))
	copy(fileImports, importSpecs)
	
	dummyFile := &ast.File{
		Name:    &ast.Ident{Name: "main"}, // package name doesn't matter for imports
		Imports: fileImports,
	}
	
	// Delete unused imports.
	for _, imp := range importSpecs {
		if !containsImport(updatedImports, imp) {
			name, path, err := getImportDetails(imp)
			if err != nil {
				return t, err
			}
			astutil.DeleteNamedImport(fset, dummyFile, name, path)
		}
	}
	// Add imports, if there are any to add.
	for _, imp := range updatedImports {
		if !containsImport(dummyFile.Imports, imp) {
			name, path, err := getImportDetails(imp)
			if err != nil {
				return t, err
			}
			astutil.AddNamedImport(fset, dummyFile, name, path)
		}
	}
	// Edge case: reinsert the import to use import syntax without parentheses.
	if len(dummyFile.Imports) == 1 {
		name, path, err := getImportDetails(dummyFile.Imports[0])
		if err != nil {
			return t, err
		}
		astutil.DeleteNamedImport(fset, dummyFile, name, path)
		astutil.AddNamedImport(fset, dummyFile, name, path)
	}
	
	// Format the imports properly
	var updatedImportsStr string
	if len(dummyFile.Imports) > 0 {
		// Create proper AST declarations for the imports
		var decls []ast.Decl
		if len(dummyFile.Imports) == 1 {
			// Single import
			decls = append(decls, &ast.GenDecl{
				Tok:   token.IMPORT,
				Specs: []ast.Spec{dummyFile.Imports[0]},
			})
		} else {
			// Multiple imports  
			var specs []ast.Spec
			for _, imp := range dummyFile.Imports {
				specs = append(specs, imp)
			}
			decls = append(decls, &ast.GenDecl{
				Tok:    token.IMPORT,
				Lparen: 1, // This indicates we want parentheses
				Specs:  specs,
			})
		}
		
		// Format just the import declarations
		var buf strings.Builder
		for _, decl := range decls {
			if err := format.Node(&buf, fset, decl); err != nil {
				return t, fmt.Errorf("failed to format import: %w", err)
			}
		}
		updatedImportsStr = strings.TrimSpace(buf.String())
	}
	
	// Reconstruct the template nodes
	t.Nodes = nil
	
	// Add the updated imports as a single node if there are any
	if updatedImportsStr != "" {
		importNode := &parser.TemplateFileGoExpression{
			Expression: parser.Expression{
				Value: updatedImportsStr,
			},
		}
		t.Nodes = append(t.Nodes, importNode)
	}
	
	// Add all non-import nodes back
	t.Nodes = append(t.Nodes, nonImportNodes...)
	
	return t, nil
}

func getImportDetails(imp *ast.ImportSpec) (name, importPath string, err error) {
	if imp.Name != nil {
		name = imp.Name.Name
	}
	if imp.Path != nil {
		importPath, err = strconv.Unquote(imp.Path.Value)
		if err != nil {
			err = fmt.Errorf("failed to unquote package path %s: %w", imp.Path.Value, err)
			return
		}
	}
	return name, importPath, nil
}

func containsImport(imports []*ast.ImportSpec, spec *ast.ImportSpec) bool {
	for _, imp := range imports {
		if imp.Path.Value == spec.Path.Value {
			// Check if both have the same name (or both are unnamed)
			impName := ""
			specName := ""
			if imp.Name != nil {
				impName = imp.Name.Name
			}
			if spec.Name != nil {
				specName = spec.Name.Name
			}
			if impName == specName {
				return true
			}
		}
	}

	return false
}
