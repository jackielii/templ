package symbolresolver

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
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
		overlay, err := generateOverlay(tf)
		if err != nil {
			return fmt.Errorf("failed to generate overlay for %s: %w", file, err)
		}

		// Store overlay with _templ.go suffix
		overlayPath := strings.TrimSuffix(file, ".templ") + "_templ.go"
		r.overlays[overlayPath] = []byte(overlay)
	}

	// Now load packages for each module separately
	for moduleRoot, files := range moduleFiles {
		// fmt.Printf("Processing module at %s with %d files\n", moduleRoot, len(files))
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
			// fmt.Printf("Error loading packages for module %s: %v\n", moduleRoot, err)
			_ = err
		}
	}

	return nil
}

// loadPackagesForModule loads all packages within a single module
func (r *SymbolResolverV2) loadPackagesForModule(moduleRoot string, packageDirs map[string][]string) error {
	if len(packageDirs) == 0 {
		return nil
	}

	cfg := &packages.Config{
		Mode:       packages.LoadSyntax,
		Dir:        moduleRoot,
		Overlay:    r.overlays,
		Env:        os.Environ(),
		BuildFlags: []string{}, // TODO: maybe support build flags in the future
	}

	// Convert directory paths to package patterns relative to module root
	patterns := make([]string, 0, len(packageDirs))
	for dir := range packageDirs {
		relPath, err := filepath.Rel(moduleRoot, dir)
		if err != nil || strings.HasPrefix(relPath, "..") {
			// If we can't make it relative or it's outside module, use absolute
			patterns = append(patterns, dir)
		} else {
			// Use relative pattern
			patterns = append(patterns, "./"+relPath)
		}
	}
	// fmt.Printf("Loading packages from %s with patterns: %v\n", moduleRoot, patterns)

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil && len(pkgs) == 0 {
		return fmt.Errorf("failed to load any packages: %w", err)
	}

	// Create a map to track which directories each package belongs to
	pkgToDirs := make(map[string][]string)

	// Process all loaded packages
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		// Debug: log packages being loaded
		// fmt.Printf("Loaded package: %s (path: %s, ID: %s, hasTypes: %v)\n", pkg.Name, pkg.PkgPath, pkg.ID, pkg.Types != nil)
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

	// TODO: once we implement proper scoping, we don't need to cache by directories
	// Now cache packages by the directories where we found templ files
	for dir := range packageDirs {
		// Find which package this directory belongs to
		found := false
		for pkgPath, dirs := range pkgToDirs {
			for _, pkgDir := range dirs {
				if pkgDir == dir {
					if pkg, ok := r.packages[pkgPath]; ok {
						r.packages[dir] = pkg
						// fmt.Printf("Cached package %s for directory %s\n", pkgPath, dir)
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}

		if !found {
			// If we still can't find a package, it might be due to edge cases
			// in how packages.Load reports directory mappings
			// fmt.Printf("Warning: could not find package for directory %s\n", dir)
		}
	}

	return nil
}

// ResolveComponent finds a component's type
// Called during code generation for element syntax like <Button />
// Returns either:
// - *types.Signature for function/method components
// - *types.Named for type components that implement templ.Component
// scope should be the appropriate scope for resolution (e.g., file scope which has package scope as parent)
func ResolveComponent(scope *types.Scope, expr ast.Expr) (types.Type, error) {
	if scope == nil {
		return nil, fmt.Errorf("scope is nil")
	}
	if expr == nil {
		return nil, fmt.Errorf("expression is nil")
	}

	// Use ResolveExpression to get the type
	typ, err := ResolveExpression(expr, scope)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve component expression: %w", err)
	}

	// Validate the component type
	if err := validateComponentType(typ); err != nil {
		return nil, err
	}

	return typ, nil
}

// GetFileScope finds the file scope for a given filename
// This is a helper for callers that need to resolve the scope from a filename
func (r *SymbolResolverV2) GetFileScope(filename string) (*types.Scope, error) {
	// Get the absolute path of the file
	absFilename, err := filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for %s: %w", filename, err)
	}

	// Get the directory for package lookup
	absDir := filepath.Dir(absFilename)

	pkg, ok := r.packages[absDir]
	if !ok {
		return nil, fmt.Errorf("package in %s not found in preprocessing cache", absDir)
	}

	if pkg.TypesInfo == nil {
		return nil, fmt.Errorf("no type information for package in %s", absDir)
	}

	// Find the file scope that includes imports
	// Look for the overlay file that corresponds to this templ file
	overlayPath := strings.TrimSuffix(absFilename, ".templ") + "_templ.go"

	var fileScope *types.Scope
	for i, file := range pkg.CompiledGoFiles {
		if file == overlayPath && i < len(pkg.Syntax) {
			// Found the overlay file - get its scope
			if pkg.TypesInfo != nil && pkg.TypesInfo.Scopes != nil {
				// The file scope is at the file level, not the package level
				// pkg.Syntax[i] is already an *ast.File
				fileNode := pkg.Syntax[i]
				fileScope = pkg.TypesInfo.Scopes[fileNode]
			}
			break
		}
	}

	if fileScope == nil {
		return nil, fmt.Errorf("file scope for %s not found in package %s", overlayPath, pkg.PkgPath)
	}

	return fileScope, nil
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

		// Recursively validate the return type
		resultType := results.At(0).Type()
		// Debug: log the result type
		// fmt.Printf("Function returns: %s (underlying: %T)\n", resultType, resultType)

		// Special case: if the return type is templ.Component, it's valid
		if named, ok := resultType.(*types.Named); ok {
			if named.Obj() != nil && named.Obj().Pkg() != nil {
				// fmt.Printf("  Return type package: %s, name: %s\n", named.Obj().Pkg().Path(), named.Obj().Name())
			}
		}

		return validateComponentType(resultType)

	case *types.Named:
		// Check if the underlying type is an interface (e.g., templ.Component)
		if _, isInterface := t.Underlying().(*types.Interface); isInterface {
			// This is a named interface type, validate it as an interface
			return validateComponentType(t.Underlying())
		}

		// Type component - check if it has a Render method
		// Get the package for this type: not sure if this is required or correct
		// TODO: find a use case where this is needed
		var pkg *types.Package
		if t.Obj() != nil {
			pkg = t.Obj().Pkg()
		}
		method, _, _ := types.LookupFieldOrMethod(t, true, pkg, "Render")
		if method == nil {
			return fmt.Errorf("type %s does not implement templ.Component (no Render method)", t)
		}
		if fn, ok := method.(*types.Func); ok {
			// Validate the Render method signature
			sig := fn.Type().(*types.Signature)
			return validateRenderSignature(sig)
		}
		return fmt.Errorf("Render is not a method on %s", t)

	case *types.Interface:
		// If it's an interface, check if it has a Render method
		// For interfaces, we typically don't have a package
		method, _, _ := types.LookupFieldOrMethod(t, true, nil, "Render")
		if method == nil {
			return fmt.Errorf("interface does not have Render method")
		}
		if fn, ok := method.(*types.Func); ok {
			sig := fn.Type().(*types.Signature)
			return validateRenderSignature(sig)
		}
		return fmt.Errorf("Render is not a method")

	case *types.Basic:
		// fmt.Printf("Basic type: %s, Kind: %v\n", t, t.Kind())
		if t.Kind() == types.Invalid {
			// fmt.Printf("  Invalid type detected - this suggests package loading issues\n")
		}
		return fmt.Errorf("basic type %s cannot be a component", t)

	default:
		// Check if it's a variable of a type that implements templ.Component
		if named, ok := typ.Underlying().(*types.Named); ok {
			return validateComponentType(named)
		}
		return fmt.Errorf("type %s is not a valid component (expected function returning templ.Component or type implementing it)", typ)
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

		// Type component - check if it has a Render method
		// Get the package for this type: not sure if this is required or correct
		// TODO: find a use case where this is needed
		// var pkg *types.Package
		// if t.Obj() != nil {
		// 	pkg = t.Obj().Pkg()
		// }
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
