package symbolresolver

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/a-h/templ/parser/v2"
	"golang.org/x/tools/go/packages"
)

// SymbolResolverV2 handles type resolution for templ templates
type SymbolResolverV2 struct {
	packages map[string]*packages.Package // key: package path or absolute directory path
	overlays map[string][]byte            // key: absolute file path -> content
}

// NewSymbolResolverV2 creates a new symbol resolver
func NewSymbolResolverV2() *SymbolResolverV2 {
	return &SymbolResolverV2{
		packages: make(map[string]*packages.Package),
		overlays: make(map[string][]byte),
	}
}

// PreprocessFiles analyzes all template files and prepares overlays
// This is called once before any code generation begins
func (r *SymbolResolverV2) PreprocessFiles(files []string) error {
	// Convert all file paths to absolute paths first
	absFiles := make([]string, 0, len(files))
	for _, file := range files {
		absFile, err := filepath.Abs(file)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for %s: %w", file, err)
		}
		absFiles = append(absFiles, absFile)
	}

	// Group files by directory to identify packages
	packageDirs := make(map[string][]string)
	for _, file := range absFiles {
		if !strings.HasSuffix(file, ".templ") {
			continue // Only process templ files
		}
		dir := filepath.Dir(file)
		packageDirs[dir] = append(packageDirs[dir], file)
	}

	// Parse each file and generate overlays
	for _, file := range absFiles {
		if !strings.HasSuffix(file, ".templ") {
			continue // Only process templ files
		}
		// Parse the template file
		tf, err := parser.Parse(file)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", file, err)
		}

		// Generate overlay
		overlay, err := r.generateOverlay(tf)
		if err != nil {
			return fmt.Errorf("failed to generate overlay for %s: %w", file, err)
		}

		// Store overlay with _templ.go suffix
		overlayPath := strings.TrimSuffix(file, ".templ") + "_templ.go"
		r.overlays[overlayPath] = []byte(overlay)
	}

	// Collect all directories for loading packages in one go
	loadPaths := slices.Collect(maps.Keys(packageDirs))
	if len(loadPaths) == 0 {
		return fmt.Errorf("no template files found to preprocess")
	}

	// Find a common directory to use as the base for loading
	// This helps packages.Load understand the module context
	var baseDir string
	if len(loadPaths) > 0 {
		baseDir = loadPaths[0]
		// Try to find the module root by looking for go.mod
		for dir := baseDir; dir != "/" && dir != ""; dir = filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				baseDir = dir
				break
			}
		}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedSyntax | packages.NeedImports | packages.NeedDeps | packages.NeedFiles,
		Dir:     baseDir,
		Overlay: r.overlays,
	}

	// Convert directory paths to package patterns relative to base
	patterns := make([]string, 0, len(loadPaths))
	for _, dir := range loadPaths {
		relPath, err := filepath.Rel(baseDir, dir)
		if err != nil || strings.HasPrefix(relPath, "..") {
			// If we can't make it relative or it's outside base, use absolute
			patterns = append(patterns, dir)
		} else {
			// Use relative pattern
			patterns = append(patterns, "./"+relPath)
		}
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		// packages.Load often returns partial results even with errors
		// Check if we got any packages at all
		// if len(pkgs) == 0 {
		// 	return fmt.Errorf("failed to load any packages: %w", err)
		// }
		// We have some packages, so we'll continue but note the error
		// This commonly happens with module boundaries or missing dependencies
		println("Warning: packages.Load encountered errors, but continuing with available packages:", err.Error())
	}

	// Process all loaded packages
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		// Skip packages with certain errors
		if len(pkg.Errors) > 0 {
			hasRealError := false
			for _, err := range pkg.Errors {
				errStr := err.Error()
				// Allow packages with only overlay files or module boundary errors
				if !strings.Contains(errStr, "_templ.go") &&
					!strings.Contains(errStr, "no Go files") &&
					!strings.Contains(errStr, "no required module provides package") &&
					!strings.Contains(errStr, "outside main module") {
					hasRealError = true
					break
				}
			}
			if hasRealError {
				return // Skip this package
			}
		}

		// Cache by package path if available
		if pkg.PkgPath != "" {
			r.packages[pkg.PkgPath] = pkg
		}

		// Also cache by directory for local lookups
		// Find the directory this package belongs to
		for _, file := range pkg.GoFiles {
			absFile, err := filepath.Abs(file)
			if err != nil {
				// Log but continue with other files
				continue
			}
			dir := filepath.Dir(absFile)
			r.packages[dir] = pkg
			break // One mapping per package is enough
		}
	})

	// Also ensure templ package is available in cache
	if _, ok := r.packages["github.com/a-h/templ"]; !ok {
		println("Loading templ package for component resolution")
		cfg := &packages.Config{
			Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo,
		}
		templPkgs, err := packages.Load(cfg, "github.com/a-h/templ")
		if err == nil && len(templPkgs) > 0 {
			r.packages["github.com/a-h/templ"] = templPkgs[0]
		}
	}

	return nil
}

// ResolveComponent finds a component's type signature
// Called during code generation for element syntax like <Button />
func (r *SymbolResolverV2) ResolveComponent(fromDir, componentName string, tf *parser.TemplateFile) (*types.Signature, error) {
	var pkgPath string
	var localName string

	// Check if component is imported (e.g., pkg.Component)
	if strings.Contains(componentName, ".") {
		parts := strings.SplitN(componentName, ".", 2)
		alias := parts[0]
		localName = parts[1]

		// Find the import path for this alias
		pkgPath = r.findImportPath(tf, alias)
		if pkgPath == "" {
			return nil, fmt.Errorf("import alias %s not found", alias)
		}
	} else {
		// Local component
		localName = componentName
	}

	// Load the package from cache
	var pkg *packages.Package
	if pkgPath != "" {
		// Load by package path
		if p, ok := r.packages[pkgPath]; ok {
			pkg = p
		} else {
			return nil, fmt.Errorf("package %s not found in preprocessing cache", pkgPath)
		}
	} else {
		// Load local package from cache
		// Convert to absolute path to match our cache keys
		absFromDir, err := filepath.Abs(fromDir)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for %s: %w", fromDir, err)
		}

		if p, ok := r.packages[absFromDir]; ok {
			pkg = p
		} else {
			return nil, fmt.Errorf("local package in %s not found in preprocessing cache", fromDir)
		}
	}

	// Find the component in the package
	if pkg.Types == nil {
		return nil, fmt.Errorf("no type information for package %s", pkg.PkgPath)
	}

	obj := pkg.Types.Scope().Lookup(localName)
	if obj == nil {
		return nil, fmt.Errorf("component %s not found in package %s", localName, pkg.PkgPath)
	}

	// Extract signature
	switch obj := obj.(type) {
	case *types.Func:
		// Function component
		return obj.Type().(*types.Signature), nil
	case *types.TypeName:
		// Struct component - look for Render method
		method, _, _ := types.LookupFieldOrMethod(obj.Type(), true, pkg.Types, "Render")
		if method == nil {
			return nil, fmt.Errorf("%s does not implement templ.Component", localName)
		}
		if fn, ok := method.(*types.Func); ok {
			return fn.Type().(*types.Signature), nil
		}
	}

	return nil, fmt.Errorf("%s is not a valid component", componentName)
}

// ResolveExpression determines the type of a Go expression
// Called during code generation for expressions like { user.Name }
func (r *SymbolResolverV2) ResolveExpression(expr ast.Expr, scope *types.Scope) (types.Type, error) {
	if expr == nil {
		return nil, fmt.Errorf("expression is nil")
	}
	if scope == nil {
		return nil, fmt.Errorf("scope is nil")
	}

	// Try to resolve the expression type using the scope
	switch e := expr.(type) {
	case *ast.Ident:
		// Simple identifier
		obj := scope.Lookup(e.Name)
		if obj == nil {
			return nil, fmt.Errorf("identifier %s not found in scope", e.Name)
		}
		return obj.Type(), nil

	case *ast.SelectorExpr:
		// Field or method access (e.g., user.Name)
		// First resolve the base expression
		baseType, err := r.ResolveExpression(e.X, scope)
		if err != nil {
			return nil, err
		}

		// Dereference pointer if needed
		if ptr, ok := baseType.(*types.Pointer); ok {
			baseType = ptr.Elem()
		}

		// Look up the field or method
		obj, _, _ := types.LookupFieldOrMethod(baseType, true, nil, e.Sel.Name)
		if obj == nil {
			return nil, fmt.Errorf("field or method %s not found", e.Sel.Name)
		}
		return obj.Type(), nil

	case *ast.IndexExpr:
		// Array/slice/map index (e.g., items[0])
		baseType, err := r.ResolveExpression(e.X, scope)
		if err != nil {
			return nil, err
		}

		switch t := baseType.Underlying().(type) {
		case *types.Array:
			return t.Elem(), nil
		case *types.Slice:
			return t.Elem(), nil
		case *types.Map:
			return t.Elem(), nil
		default:
			return nil, fmt.Errorf("cannot index type %s", baseType)
		}

	case *ast.CallExpr:
		// Function call
		fnType, err := r.ResolveExpression(e.Fun, scope)
		if err != nil {
			return nil, err
		}

		sig, ok := fnType.(*types.Signature)
		if !ok {
			return nil, fmt.Errorf("not a function type")
		}

		results := sig.Results()
		if results.Len() == 0 {
			return nil, fmt.Errorf("function has no return values")
		}
		return results.At(0).Type(), nil

	case *ast.BasicLit:
		// Literal values
		switch e.Kind {
		case token.INT:
			return types.Typ[types.Int], nil
		case token.FLOAT:
			return types.Typ[types.Float64], nil
		case token.STRING:
			return types.Typ[types.String], nil
		case token.CHAR:
			return types.Typ[types.Rune], nil
		default:
			return nil, fmt.Errorf("unknown literal kind: %v", e.Kind)
		}

	default:
		return nil, fmt.Errorf("unsupported expression type: %T", expr)
	}
}

// findImportPath finds the import path for a given alias in the template file
func (r *SymbolResolverV2) findImportPath(tf *parser.TemplateFile, alias string) string {
	for _, node := range tf.Nodes {
		if goExpr, ok := node.(*parser.TemplateFileGoExpression); ok {
			if goExpr.Expression.AstNode != nil {
				if genDecl, ok := goExpr.Expression.AstNode.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
					for _, spec := range genDecl.Specs {
						if impSpec, ok := spec.(*ast.ImportSpec); ok {
							// Check if this import has the alias we're looking for
							var importAlias string
							if impSpec.Name != nil {
								importAlias = impSpec.Name.Name
							} else {
								// Default alias is the last part of the path
								path := strings.Trim(impSpec.Path.Value, `"`)
								parts := strings.Split(path, "/")
								importAlias = parts[len(parts)-1]
							}
							if importAlias == alias {
								return strings.Trim(impSpec.Path.Value, `"`)
							}
						}
					}
				}
			}
		}
	}
	return ""
}

// generateOverlay creates a Go stub file for a templ template
func (r *SymbolResolverV2) generateOverlay(tf *parser.TemplateFile) (string, error) {
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
