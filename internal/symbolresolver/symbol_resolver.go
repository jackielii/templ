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

	// First, group files by module (identified by go.mod location)
	moduleFiles := make(map[string][]string) // module root -> files
	for _, file := range absFiles {
		if !strings.HasSuffix(file, ".templ") {
			continue // Only process templ files
		}

		// Find the module root for this file
		moduleRoot := findModuleRoot(filepath.Dir(file))
		moduleFiles[moduleRoot] = append(moduleFiles[moduleRoot], file)
	}

	// Parse each file and generate overlays (do this before loading packages)
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

	// Now load packages for each module separately
	for moduleRoot, files := range moduleFiles {
		// Group files by package within this module
		packageDirs := make(map[string][]string)
		for _, file := range files {
			dir := filepath.Dir(file)
			packageDirs[dir] = append(packageDirs[dir], file)
		}

		// Load packages for this module
		if err := r.loadPackagesForModule(moduleRoot, packageDirs); err != nil {
			// Continue with other modules - one module's failure shouldn't stop others
			// The error is already descriptive from loadPackagesForModule
			_ = err
		}
	}

	return nil
}

// loadPackagesForModule loads all packages within a single module
func (r *SymbolResolverV2) loadPackagesForModule(moduleRoot string, packageDirs map[string][]string) error {
	loadPaths := slices.Collect(maps.Keys(packageDirs))
	if len(loadPaths) == 0 {
		return nil
	}

	// Load packages for this module

	cfg := &packages.Config{
		Mode:    packages.LoadSyntax,
		Dir:     moduleRoot,
		Overlay: r.overlays,
	}

	// Convert directory paths to package patterns relative to module root
	patterns := make([]string, 0, len(loadPaths))
	for _, dir := range loadPaths {
		relPath, err := filepath.Rel(moduleRoot, dir)
		if err != nil || strings.HasPrefix(relPath, "..") {
			// If we can't make it relative or it's outside module, use absolute
			patterns = append(patterns, dir)
		} else {
			// Use relative pattern
			patterns = append(patterns, "./"+relPath)
		}
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil && len(pkgs) == 0 {
		return fmt.Errorf("failed to load any packages: %w", err)
	}

	// Create a map to track which directories each package belongs to
	pkgToDirs := make(map[string][]string)

	// Process all loaded packages
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		// Skip packages with errors - packages.Visit handles dependencies correctly
		// even with errors, so we can still cache what we get
		if len(pkg.Errors) > 0 {
			// Log errors for debugging but don't skip the package
			// Most errors are benign (e.g., unused imports in overlays)
			// fmt.Printf("Package %s has errors: %v\n", pkg.ID, pkg.Errors)
		}

		// Cache by package path if available
		if pkg.PkgPath != "" {
			r.packages[pkg.PkgPath] = pkg
		}

		// Also cache by ID (which might be a relative path like ./foo/bar)
		if pkg.ID != "" && pkg.ID != pkg.PkgPath {
			r.packages[pkg.ID] = pkg
		}

		// Track which directories might belong to this package
		for _, file := range pkg.GoFiles {
			absFile, err := filepath.Abs(file)
			if err == nil {
				dir := filepath.Dir(absFile)
				pkgToDirs[pkg.PkgPath] = append(pkgToDirs[pkg.PkgPath], dir)
			}
		}

		// Also check CompiledGoFiles which includes overlays
		for _, file := range pkg.CompiledGoFiles {
			if strings.HasSuffix(file, "_templ.go") {
				// This is an overlay file, extract the directory
				dir := filepath.Dir(strings.TrimSuffix(file, "_templ.go") + ".templ")
				pkgToDirs[pkg.PkgPath] = append(pkgToDirs[pkg.PkgPath], dir)
			}
		}
	})

	// Now cache packages by the directories where we found templ files
	for dir := range packageDirs {
		// Find which package this directory belongs to
		found := false
		for pkgPath, dirs := range pkgToDirs {
			for _, pkgDir := range dirs {
				if pkgDir == dir {
					if pkg, ok := r.packages[pkgPath]; ok {
						r.packages[dir] = pkg
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}

		// If we still can't find a package, it might be due to edge cases
		// in how packages.Load reports directory mappings
		// fmt.Printf("Warning: could not find package for directory %s\n", dir)
	}

	return nil
}

// ResolveComponent finds a component's type
// Called during code generation for element syntax like <Button />
// Returns either:
// - *types.Signature for function/method components
// - *types.Named for type components that implement templ.Component
func (r *SymbolResolverV2) ResolveComponent(fromFile string, expr ast.Expr) (types.Type, error) {
	// Get the absolute path of the file
	absFromFile, err := filepath.Abs(fromFile)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for %s: %w", fromFile, err)
	}

	// Get the directory for package lookup
	fromDir := filepath.Dir(absFromFile)

	pkg, ok := r.packages[fromDir]
	if !ok {
		return nil, fmt.Errorf("package in %s not found in preprocessing cache", fromDir)
	}

	if pkg.TypesInfo == nil {
		return nil, fmt.Errorf("no type information for package in %s", fromDir)
	}

	// Find the file scope that includes imports
	// Look for the overlay file that corresponds to this templ file
	overlayPath := strings.TrimSuffix(absFromFile, ".templ") + "_templ.go"

	var fileScope *types.Scope
	// fmt.Printf("Looking for overlay: %s\n", overlayPath)
	// fmt.Printf("CompiledGoFiles: %v\n", pkg.CompiledGoFiles)
	for i, file := range pkg.CompiledGoFiles {
		if file == overlayPath && i < len(pkg.Syntax) {
			// Found the overlay file - get its scope
			if pkg.TypesInfo != nil && pkg.TypesInfo.Scopes != nil {
				// The file scope is at the file level, not the package level
				// pkg.Syntax[i] is already an *ast.File
				fileNode := pkg.Syntax[i]
				fileScope = pkg.TypesInfo.Scopes[fileNode]
				// fmt.Printf("Found file scope for %s\n", overlayPath)
				// if fileScope != nil {
				// 	fmt.Printf("File scope names: %v\n", fileScope.Names())
				// }
			}
			break
		}
	}

	// If we couldn't find a file-specific scope, use the package scope
	if fileScope == nil {
		// fmt.Printf("Using package scope as fallback\n")
		fileScope = pkg.Types.Scope()
		// if fileScope != nil {
		// 	fmt.Printf("Package scope names: %v\n", fileScope.Names())
		// }
	}

	// Use ResolveExpression to get the type
	// Pass file scope which has package scope as its parent
	typ, err := ResolveExpression(expr, fileScope)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve component expression: %w", err)
	}

	// Validate the component type
	if err := validateComponentType(typ); err != nil {
		return nil, err
	}

	return typ, nil
}

// validateComponentType validates that a type can be used as a component
func validateComponentType(typ types.Type) error {
	switch t := typ.(type) {
	case *types.Signature:
		// Function component - validate it returns templ.Component
		results := t.Results()
		if results.Len() != 1 {
			return fmt.Errorf("component function should return exactly 1 value, got %d", results.Len())
		}
		// In a real implementation, we'd check if return type is templ.Component
		// For now, we accept any single return value
		return nil

	case *types.Named:
		// Type component - check if it has a Render method
		method, _, _ := types.LookupFieldOrMethod(t, true, nil, "Render")
		if method == nil {
			return fmt.Errorf("type %s does not implement templ.Component (no Render method)", t)
		}
		if fn, ok := method.(*types.Func); ok {
			// Validate the Render method signature
			sig := fn.Type().(*types.Signature)
			return validateRenderSignature(sig)
		}
		return fmt.Errorf("Render is not a method on %s", t)

	default:
		// Check if it's a variable of a type that implements templ.Component
		if named, ok := typ.Underlying().(*types.Named); ok {
			return validateComponentType(named)
		}
		return fmt.Errorf("type %s is not a valid component (expected function or type implementing templ.Component)", typ)
	}
}

// validateRenderSignature checks if a signature matches the Render method of templ.Component
// Should be: Render(ctx context.Context, w io.Writer) error
func validateRenderSignature(sig *types.Signature) error {
	// Should have exactly 2 parameters
	params := sig.Params()
	if params.Len() != 2 {
		return fmt.Errorf("Render method should have exactly 2 parameters, got %d", params.Len())
	}

	// First param should be context.Context
	ctxType := params.At(0).Type()
	if ctxType.String() != "context.Context" {
		return fmt.Errorf("first parameter should be context.Context, got %s", ctxType)
	}

	// Second param should be io.Writer
	writerType := params.At(1).Type()
	if writerType.String() != "io.Writer" {
		return fmt.Errorf("second parameter should be io.Writer, got %s", writerType)
	}

	// Should return error
	results := sig.Results()
	if results.Len() != 1 {
		return fmt.Errorf("Render method should return exactly 1 value, got %d", results.Len())
	}
	if results.At(0).Type().String() != "error" {
		return fmt.Errorf("Render method should return error, got %s", results.At(0).Type())
	}

	return nil
}

// ResolveExpression determines the type of a Go expression
// Called during code generation for expressions like { user.Name }
// scope should be the innermost scope (e.g., file scope which has package scope as parent)
func ResolveExpression(expr ast.Expr, scope *types.Scope) (types.Type, error) {
	if expr == nil {
		return nil, fmt.Errorf("expression is nil")
	}
	if scope == nil {
		return nil, fmt.Errorf("scope is nil")
	}

	// Try to resolve the expression type using the scope
	switch e := expr.(type) {
	case *ast.Ident:
		// Simple identifier - use LookupParent to search up the scope hierarchy
		_, obj := scope.LookupParent(e.Name, token.NoPos)
		if obj == nil {
			return nil, fmt.Errorf("identifier %s not found in scope", e.Name)
		}
		return obj.Type(), nil

	case *ast.SelectorExpr:
		// This could be either:
		// 1. Package-qualified identifier (e.g., pkg.Component)
		// 2. Field or method access (e.g., user.Name)

		// Check if X is an identifier - might be a package name
		if ident, ok := e.X.(*ast.Ident); ok {
			// Look up the identifier - imports are in file scope, other identifiers in package scope
			_, obj := scope.LookupParent(ident.Name, token.NoPos)
			if obj != nil {
				// Check if this is a package name (imported package)
				if pkgName, ok := obj.(*types.PkgName); ok {
					// Look up the identifier in the imported package
					importedPkg := pkgName.Imported()
					if importedPkg == nil {
						return nil, fmt.Errorf("imported package %s not found", ident.Name)
					}
					obj := importedPkg.Scope().Lookup(e.Sel.Name)
					if obj == nil {
						return nil, fmt.Errorf("%s not found in package %s", e.Sel.Name, ident.Name)
					}
					return obj.Type(), nil
				}
			}
		}

		// Not a package - resolve as field/method access
		baseType, err := ResolveExpression(e.X, scope)
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
		baseType, err := ResolveExpression(e.X, scope)
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
		fnType, err := ResolveExpression(e.Fun, scope)
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

// findModuleRoot finds the go.mod file for a given directory
func findModuleRoot(dir string) string {
	for current := dir; current != "/" && current != ""; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
	}
	// If no go.mod found, return the original directory
	return dir
}
