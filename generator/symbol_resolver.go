package generator

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/a-h/templ/parser/v2"
	"golang.org/x/tools/go/packages"
)

// ComponentSignature represents a templ component's function signature or struct fields
type ComponentSignature struct {
	PackagePath   string
	Name          string
	QualifiedName string          // For functions: pkgPath.Name, For structs: pkgPath.TypeName
	Parameters    []ParameterInfo // For functions: parameters, For structs: exported fields
	IsStruct      bool
	IsPointerRecv bool
}

// ParameterInfo represents a function parameter or struct field with rich type information
type ParameterInfo struct {
	Name         string
	Type         string // String representation for display/debugging
	IsComponent  bool   // Pre-computed: implements templ.Component interface
	IsAttributer bool   // Pre-computed: implements templ.Attributer interface
	IsPointer    bool   // Pre-computed: is a pointer type
	IsSlice      bool   // Pre-computed: is a slice type
	IsMap        bool   // Pre-computed: is a map type
	IsString     bool   // Pre-computed: is string type
	IsBool       bool   // Pre-computed: is bool type
}

// ComponentResolutionError represents an error during component resolution with position information
type ComponentResolutionError struct {
	Err      error
	Position parser.Position
	FileName string
}

func (e ComponentResolutionError) Error() string {
	if e.FileName == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s:%d:%d: %v", e.FileName, e.Position.Line, e.Position.Col, e.Err)
}

// TypeInfo contains comprehensive type information
type TypeInfo struct {
	FullType     string // e.g., "templ.Component"
	Package      string // e.g., "github.com/a-h/templ"
	IsInterface  bool
	IsPointer    bool
	IsComponent  bool // Pre-computed: implements templ.Component
	IsAttributer bool // Pre-computed: implements templ.Attributer
	IsError      bool // Pre-computed: is error type
	IsString     bool // Pre-computed: is string type
	IsBool       bool // Pre-computed: is bool type
	IsSlice      bool // Pre-computed: is slice type
	IsMap        bool // Pre-computed: is map type
}

// SymbolResolver automatically detects module roots and provides unified resolution
// for both templ templates and Go components across packages
type SymbolResolver struct {
	signatures      map[string]ComponentSignature // Cache keyed by fully qualified names
	overlay         map[string][]byte             // Go file overlays for templ templates
	packageCache    map[string]*packages.Package  // Cache of loaded packages by directory
	currentPackage  *packages.Package             // Current package being processed
	currentPkgPath  string                        // Current package path
}

// newSymbolResolver creates a new symbol resolver
func newSymbolResolver() SymbolResolver {
	return SymbolResolver{
		signatures:    make(map[string]ComponentSignature),
		overlay:       make(map[string][]byte),
		packageCache:  make(map[string]*packages.Package),
	}
}

// ResolveElementComponent resolves a component for element syntax during code generation
// This is the main entry point for element component resolution
func (r *SymbolResolver) ResolveElementComponent(fromDir, currentPkg string, componentName string, tf *parser.TemplateFile) (ComponentSignature, error) {
	// First check cache
	if sig, ok := r.signatures[componentName]; ok {
		return sig, nil
	}

	// Parse component name to determine resolution strategy
	var packagePath, localName string
	if strings.Contains(componentName, ".") {
		parts := strings.Split(componentName, ".")
		if len(parts) == 2 {
			// Check if first part is an import alias
			importPath := r.resolveImportAlias(parts[0], tf)
			if importPath != "" {
				// Cross-package component: alias.Component
				packagePath = importPath
				localName = parts[1]
			} else {
				// Could be struct variable method: structVar.Method
				localName = componentName // Try as local component first
			}
		} else {
			// Complex dotted name - treat as local
			localName = componentName
		}
	} else {
		// Simple name - definitely local
		localName = componentName
	}

	var sig ComponentSignature
	var err error

	if packagePath != "" {
		// Cross-package resolution
		sig, err = r.ResolveComponentFrom(fromDir, packagePath, localName)
	} else {
		// Local resolution - try multiple strategies
		sig, err = r.resolveLocalComponent(fromDir, currentPkg, localName, tf)
	}

	if err != nil {
		return ComponentSignature{}, err
	}

	// Cache with original component name for future lookups
	sig.QualifiedName = componentName
	r.signatures[componentName] = sig

	return sig, nil
}

// resolveImportAlias resolves an import alias to its full package path
func (r *SymbolResolver) resolveImportAlias(alias string, tf *parser.TemplateFile) string {
	for _, node := range tf.Nodes {
		if goExpr, ok := node.(*parser.TemplateFileGoExpression); ok {
			if strings.Contains(goExpr.Expression.Value, "import") {
				if path := r.parseImportPath(goExpr.Expression.Value, alias); path != "" {
					return path
				}
			}
		}
	}
	return ""
}

// parseImportPath extracts the import path for a specific alias using Go AST parser
func (r *SymbolResolver) parseImportPath(goCode, packageAlias string) string {
	// Try to parse as a complete Go file first
	fullGoCode := "package main\n" + goCode
	fset := token.NewFileSet()

	astFile, err := goparser.ParseFile(fset, "", fullGoCode, goparser.ImportsOnly)
	if err != nil {
		// If that fails, try parsing just the import block
		if strings.Contains(goCode, "import (") {
			start := strings.Index(goCode, "import (")
			if start != -1 {
				end := strings.Index(goCode[start:], ")")
				if end != -1 {
					importBlock := goCode[start : start+end+1]
					fullGoCode = "package main\n" + importBlock
					astFile, err = goparser.ParseFile(fset, "", fullGoCode, goparser.ImportsOnly)
				}
			}
		}
		if err != nil {
			return ""
		}
	}

	// Extract import path for the specific alias from AST
	for _, imp := range astFile.Imports {
		if imp.Path != nil {
			pkgPath := strings.Trim(imp.Path.Value, `"`)
			var alias string

			if imp.Name != nil {
				// Explicit alias: import alias "path"
				alias = imp.Name.Name
			} else {
				// No explicit alias: import "path" -> derive alias from path
				if lastSlash := strings.LastIndex(pkgPath, "/"); lastSlash != -1 {
					alias = pkgPath[lastSlash+1:]
				}
			}

			if alias == packageAlias {
				return pkgPath
			}
		}
	}

	return ""
}

// resolveLocalComponent resolves a local component using package loading
func (r *SymbolResolver) resolveLocalComponent(fromDir, currentPkg, componentName string, tf *parser.TemplateFile) (ComponentSignature, error) {
	// For dotted names, check if it's a struct method first
	if strings.Contains(componentName, ".") {
		if sig, ok := r.resolveStructMethod(componentName, tf, fromDir); ok {
			return sig, nil
		}
	}
	
	// Use package resolution with overlays
	if currentPkg == "" {
		// Try to determine current package from directory
		var err error
		currentPkg, err = r.getPackagePathFromDir(fromDir)
		if err != nil {
			return ComponentSignature{}, fmt.Errorf("failed to determine package path: %w", err)
		}
	}
	
	sig, err := r.ResolveComponentFrom(fromDir, currentPkg, componentName)
	if err == nil {
		return sig, nil
	}
	
	return ComponentSignature{}, fmt.Errorf("component %s not found: %w", componentName, err)
}

// resolveStructMethod resolves struct variable method components like structVar.Method
func (r *SymbolResolver) resolveStructMethod(componentName string, tf *parser.TemplateFile, fromDir string) (ComponentSignature, bool) {
	parts := strings.Split(componentName, ".")
	if len(parts) < 2 {
		return ComponentSignature{}, false
	}
	
	varName := parts[0]
	methodName := strings.Join(parts[1:], ".")
	
	// Ensure package is loaded with overlays
	pkg, err := r.ensurePackageLoaded(fromDir)
	if err != nil {
		return ComponentSignature{}, false
	}
	
	// Look for the variable in the package scope
	obj := pkg.Types.Scope().Lookup(varName)
	if obj == nil {
		return ComponentSignature{}, false
	}
	
	// Get the type of the variable
	varType := obj.Type()
	
	// If it's a pointer, get the element type
	if ptr, ok := varType.(*types.Pointer); ok {
		varType = ptr.Elem()
	}
	
	// Check if it's a named type
	if _, ok := varType.(*types.Named); !ok {
		return ComponentSignature{}, false
	}
	
	// Look for the method
	methodObj, _, _ := types.LookupFieldOrMethod(varType, true, pkg.Types, methodName)
	if methodObj == nil {
		return ComponentSignature{}, false
	}
	
	// Extract signature
	sig, err := r.extractComponentSignature(methodObj, pkg.PkgPath, parser.Position{}, "")
	if err != nil {
		return ComponentSignature{}, false
	}
	
	// Create alias for future lookups
	sig.QualifiedName = componentName
	r.signatures[componentName] = sig
	return sig, true
}

// extractVariableType extracts the type from a variable declaration using AST parsing
func (r *SymbolResolver) extractVariableType(goCode, varName string) string {
	// Parse the Go code to extract variable type information
	src := "package main\n" + goCode
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, "", src, goparser.AllErrors)
	if err != nil || node == nil {
		// Fallback to simple string parsing
		return r.extractVariableTypeSimple(goCode, varName)
	}

	// Look for variable declarations in AST
	for _, decl := range node.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.VAR {
			for _, spec := range genDecl.Specs {
				if valueSpec, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range valueSpec.Names {
						if name.Name == varName && valueSpec.Type != nil {
							return r.astTypeToString(valueSpec.Type)
						}
					}
				}
			}
		}
	}

	return ""
}

// extractVariableTypeSimple provides fallback simple string parsing for variable types
func (r *SymbolResolver) extractVariableTypeSimple(goCode, varName string) string {
	lines := strings.Split(goCode, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Handle "var varName TypeName"
		if strings.HasPrefix(line, "var "+varName+" ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return parts[2]
			}
		}

		// Handle "varName := TypeName{}" or "varName = TypeName{}"
		if strings.Contains(line, varName+" :=") || strings.Contains(line, varName+" =") {
			// Extract type from constructor call like "StructComponent{}"
			if idx := strings.Index(line, "{"); idx != -1 {
				beforeBrace := line[:idx]
				parts := strings.Fields(beforeBrace)
				if len(parts) >= 3 {
					return parts[len(parts)-1]
				}
			}
		}
	}

	return ""
}

// ResolveComponentFrom resolves a component starting from a specific directory
// This is the primary method used during code generation
func (r *SymbolResolver) ResolveComponentFrom(fromDir, pkgPath, componentName string) (ComponentSignature, error) {
	return r.ResolveComponentWithPosition(fromDir, pkgPath, componentName, parser.Position{}, "")
}

// ResolveComponentWithPosition resolves a component with position information for error reporting
func (r *SymbolResolver) ResolveComponentWithPosition(fromDir, pkgPath, componentName string, pos parser.Position, fileName string) (ComponentSignature, error) {
	// Generate fully qualified name as cache key
	var qualifiedName string
	if pkgPath == "" {
		// For local components, we'll determine the package path during resolution
		qualifiedName = "" // Will be set after we determine the actual package
	} else {
		qualifiedName = pkgPath + "." + componentName
	}

	// Check cache first if we have a qualified name
	if qualifiedName != "" {
		if sig, ok := r.signatures[qualifiedName]; ok {
			return sig, nil
		}
	}

	// For cross-package components, we need to load from the target package directory
	loadDir := fromDir
	if pkgPath != "" && !strings.HasPrefix(pkgPath, ".") {
		// This is a cross-package import, we need to find the directory for this package
		// For now, use packages.Load to find the directory
		cfg := &packages.Config{
			Mode: packages.NeedFiles,
			Dir:  fromDir,
		}
		pkgs, err := packages.Load(cfg, pkgPath)
		if err == nil && len(pkgs) > 0 && len(pkgs[0].GoFiles) > 0 {
			loadDir = filepath.Dir(pkgs[0].GoFiles[0])
		}
	}
	
	// Use ensurePackageLoaded which properly handles overlays
	pkg, err := r.ensurePackageLoaded(loadDir)
	if err != nil {
		baseErr := fmt.Errorf("failed to load package %s: %w", pkgPath, err)
		if fileName != "" {
			return ComponentSignature{}, ComponentResolutionError{Err: baseErr, Position: pos, FileName: fileName}
		}
		return ComponentSignature{}, baseErr
	}
	// Allow packages with errors if they're compilation errors from generated files
	if len(pkg.Errors) > 0 {
		// Check if errors are from _templ.go files - if so, we can ignore them
		hasNonTemplErrors := false
		for _, err := range pkg.Errors {
			errStr := err.Error()
			if !strings.Contains(errStr, "_templ.go") {
				hasNonTemplErrors = true
				break
			}
		}
		if hasNonTemplErrors {
			baseErr := fmt.Errorf("package %s has non-generated file errors: %v", pkgPath, pkg.Errors)
			if fileName != "" {
				return ComponentSignature{}, ComponentResolutionError{Err: baseErr, Position: pos, FileName: fileName}
			}
			return ComponentSignature{}, baseErr
		}
	}

	// Look for the component in the package's type information
	if pkg.Types == nil {
		baseErr := fmt.Errorf("no type information available for package %s", pkgPath)
		if fileName != "" {
			return ComponentSignature{}, ComponentResolutionError{Err: baseErr, Position: pos, FileName: fileName}
		}
		return ComponentSignature{}, baseErr
	}

	obj := pkg.Types.Scope().Lookup(componentName)
	if obj == nil {
		baseErr := fmt.Errorf("component %s not found in package %s", componentName, pkgPath)
		if fileName != "" {
			return ComponentSignature{}, ComponentResolutionError{Err: baseErr, Position: pos, FileName: fileName}
		}
		return ComponentSignature{}, baseErr
	}

	// Extract signature using the sophisticated logic from SymbolResolver
	sig, err := r.extractComponentSignature(obj, pkgPath, pos, fileName)
	if err != nil {
		return ComponentSignature{}, err
	}

	// Set the fully qualified name for caching
	if qualifiedName == "" {
		// For local components, use the actual package path we resolved
		qualifiedName = pkg.PkgPath + "." + componentName
		sig.QualifiedName = qualifiedName
	} else {
		sig.QualifiedName = qualifiedName
	}

	// Cache using fully qualified name
	r.signatures[qualifiedName] = sig
	return sig, nil
}

// ResolveComponent resolves from current working directory (for compatibility)
func (r *SymbolResolver) ResolveComponent(pkgPath, componentName string) (ComponentSignature, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return ComponentSignature{}, fmt.Errorf("failed to get working directory: %w", err)
	}
	return r.ResolveComponentFrom(cwd, pkgPath, componentName)
}

// processTemplFiles finds and parses .templ files in the package directory,
// generating Go stub overlays for each templ template

// generateOverlay creates Go stub code for templ templates
func (r *SymbolResolver) generateOverlay(tf *parser.TemplateFile, pkgName string) string {
	var sb strings.Builder
	
	sb.WriteString(fmt.Sprintf("package %s\n\n", pkgName))
	
	// First pass: collect imports and write top-level Go code
	hasImports := false
	for _, node := range tf.Nodes {
		if goExpr, ok := node.(*parser.TemplateFileGoExpression); ok {
			// This includes imports, type definitions, variables, etc.
			sb.WriteString(goExpr.Expression.Value)
			sb.WriteString("\n\n")
			if strings.Contains(goExpr.Expression.Value, "import") {
				hasImports = true
			}
		}
	}
	
	// Add required imports if not already present
	if !hasImports {
		sb.WriteString("import (\n")
		sb.WriteString("\t\"github.com/a-h/templ\"\n")
		sb.WriteString("\t\"context\"\n")
		sb.WriteString("\t\"io\"\n")
		sb.WriteString(")\n\n")
	}
	
	// Second pass: generate stubs for templates
	for _, node := range tf.Nodes {
		switch n := node.(type) {
		case *parser.HTMLTemplate:
			// Parse the template signature to extract name and parameters
			name, _, err := r.parseTemplateSignatureFromAST(n.Expression.Value)
			if err != nil || name == "" {
				// Fallback: try to extract just the name
				parts := strings.Fields(n.Expression.Value)
				if len(parts) > 0 {
					name = strings.TrimSuffix(parts[0], "(")
				}
			}
			_ = name // Name extracted for potential future use
			
			// Generate proper function signature
			sb.WriteString("func ")
			sb.WriteString(n.Expression.Value)
			sb.WriteString(" templ.Component {\n")
			sb.WriteString("\treturn templ.ComponentFunc(func(ctx context.Context, w io.Writer) error { return nil })\n")
			sb.WriteString("}\n\n")
			
		case *parser.CSSTemplate:
			// CSS templates become functions returning templ.CSSClass
			sb.WriteString("func ")
			sb.WriteString(n.Name)
			sb.WriteString("() templ.CSSClass {\n")
			sb.WriteString("\treturn templ.ComponentCSSClass{}\n")
			sb.WriteString("}\n\n")
			
		case *parser.ScriptTemplate:
			// Script templates with proper signatures
			sb.WriteString("func ")
			sb.WriteString(n.Name.Value)
			sb.WriteString("(")
			sb.WriteString(n.Parameters.Value)
			sb.WriteString(") templ.ComponentScript {\n")
			sb.WriteString("\treturn templ.ComponentScript{}\n")
			sb.WriteString("}\n\n")
		}
	}
	
	return sb.String()
}

// extractComponentSignature extracts component signature with position-aware error handling
func (r *SymbolResolver) extractComponentSignature(obj types.Object, pkgPath string, pos parser.Position, fileName string) (ComponentSignature, error) {
	var paramInfo []ParameterInfo
	var isStruct, isPointerRecv bool

	// The component can be either a function or a type that implements templ.Component
	if fn, ok := obj.(*types.Func); ok {
		sig := fn.Type().(*types.Signature)
		params := sig.Params()
		paramInfo = make([]ParameterInfo, 0, params.Len())

		for i := 0; i < params.Len(); i++ {
			param := params.At(i)
			paramInfo = append(paramInfo, r.analyzeParameterType(param.Name(), param.Type()))
		}
	} else {
		typeName, ok := obj.(*types.TypeName)
		if !ok {
			baseErr := fmt.Errorf("%s is neither a function nor a type", obj.Name())
			if fileName != "" {
				return ComponentSignature{}, ComponentResolutionError{Err: baseErr, Position: pos, FileName: fileName}
			}
			return ComponentSignature{}, baseErr
		}

		isStruct, isPointerRecv = r.implementsComponent(typeName.Type(), typeName.Pkg())
		if !isStruct {
			baseErr := fmt.Errorf("%s does not implement templ.Component interface", obj.Name())
			if fileName != "" {
				return ComponentSignature{}, ComponentResolutionError{Err: baseErr, Position: pos, FileName: fileName}
			}
			return ComponentSignature{}, baseErr
		}

		// Extract struct fields for struct components
		paramInfo = r.extractStructFieldsWithTypeAnalysis(typeName.Type())
	}

	return ComponentSignature{
		PackagePath:   pkgPath,
		Name:          obj.Name(),
		Parameters:    paramInfo,
		IsStruct:      isStruct,
		IsPointerRecv: isPointerRecv,
	}, nil
}

// extractSignature extracts component signature information from a types.Object (legacy method)
func (r *SymbolResolver) extractSignature(obj types.Object, pkgPath string) ComponentSignature {
	sig, _ := r.extractComponentSignature(obj, pkgPath, parser.Position{}, "")
	return sig
}

// extractFunctionSignature extracts signature from a function (templ template or Go function)
func (r *SymbolResolver) extractFunctionSignature(fn *types.Func, pkgPath string) ComponentSignature {
	sig := fn.Type().(*types.Signature)
	params := sig.Params()

	paramInfo := make([]ParameterInfo, 0, params.Len())
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		paramInfo = append(paramInfo, r.analyzeParameterType(param.Name(), param.Type()))
	}

	return ComponentSignature{
		PackagePath: pkgPath,
		Name:        fn.Name(),
		Parameters:  paramInfo,
		IsStruct:    false,
	}
}

// extractTypeSignature extracts signature from a type (struct component)
func (r *SymbolResolver) extractTypeSignature(tn *types.TypeName, pkgPath string) ComponentSignature {
	// First verify this type actually implements the Component interface
	isStruct, isPointerRecv := r.implementsComponent(tn.Type(), tn.Pkg())
	if !isStruct {
		// Return empty signature if it doesn't implement Component
		return ComponentSignature{}
	}

	sig := ComponentSignature{
		PackagePath:   pkgPath,
		Name:          tn.Name(),
		IsStruct:      true,
		IsPointerRecv: isPointerRecv,
		Parameters:    r.extractStructFields(tn.Type()),
	}

	return sig
}

// ExtractSignatures walks through a templ file and extracts all template signatures
// Register registers a template file for potential symbol resolution
// This method generates an overlay that makes the templ file the single source of truth
func (r *SymbolResolver) Register(tf *parser.TemplateFile, fileName string) error {
	// Extract package name from the template file
	pkgName := ""
	if tf.Package.Expression.Value != "" {
		pkgName = strings.TrimPrefix(tf.Package.Expression.Value, "package ")
		pkgName = strings.TrimSpace(pkgName)
	}
	
	if pkgName == "" {
		return fmt.Errorf("no package declaration found in template file")
	}
	
	// Generate overlay with the same name as the output file
	// Ensure fileName is absolute first
	absFileName := fileName
	if !filepath.IsAbs(fileName) {
		var err error
		absFileName, err = filepath.Abs(fileName)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}
	}
	overlayPath := strings.TrimSuffix(absFileName, ".templ") + "_templ.go"
	
	overlayContent := r.generateOverlay(tf, pkgName)
	r.overlay[overlayPath] = []byte(overlayContent)
	
	return nil
}

// processTemplFiles finds and processes all templ files in a directory
func (r *SymbolResolver) processTemplFiles(dir string) error {
	// Find all .templ files in the directory
	templFiles, err := filepath.Glob(filepath.Join(dir, "*.templ"))
	if err != nil {
		return err
	}
	
	for _, templFile := range templFiles {
		// Skip if we already have an overlay for this file
		overlayPath := strings.TrimSuffix(templFile, ".templ") + "_templ.go"
		if _, exists := r.overlay[overlayPath]; exists {
			continue
		}
		
		// Parse the templ file
		content, err := os.ReadFile(templFile)
		if err != nil {
			continue // Skip files we can't read
		}
		
		tf, err := parser.ParseString(string(content))
		if err != nil {
			continue // Skip files we can't parse
		}
		
		// Register the file to create overlay
		if err := r.Register(tf, templFile); err != nil {
			continue // Skip files we can't register
		}
	}
	
	return nil
}

// ensurePackageLoaded lazily loads the package with full type information
func (r *SymbolResolver) ensurePackageLoaded(fromDir string) (*packages.Package, error) {
	// Check cache first
	if pkg, ok := r.packageCache[fromDir]; ok {
		return pkg, nil
	}
	
	// First, process all templ files in this directory to generate overlays
	if err := r.processTemplFiles(fromDir); err != nil {
		// Don't fail if we can't process templ files, just log it
		// The package might still be loadable without them
	}
	
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedTypesInfo |
			packages.NeedModule,
		Dir:     fromDir,
		Overlay: r.overlay,
	}
	
	// Process templ files in the package directory to generate overlays
	// This ensures we have stubs for all templates in the package
	if err := r.processTemplFiles(fromDir); err != nil {
		// Don't fail - we can still try to load the package
	}
	
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("failed to load package: %w", err)
	}
	
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found")
	}
	
	pkg := pkgs[0]
	// Allow packages with errors if they're from _templ.go files
	if len(pkg.Errors) > 0 {
		hasNonTemplErrors := false
		for _, err := range pkg.Errors {
			errStr := err.Error()
			if !strings.Contains(errStr, "_templ.go") {
				hasNonTemplErrors = true
				break
			}
		}
		if hasNonTemplErrors {
			return nil, fmt.Errorf("package has errors: %v", pkg.Errors)
		}
	}
	
	r.packageCache[fromDir] = pkg
	return pkg, nil
}


// GetLocalTemplate returns a local template signature by name
// This method is for backward compatibility - prefer using fully qualified names
func (r *SymbolResolver) GetLocalTemplate(name string) (ComponentSignature, bool) {
	// Check signatures cache
	for qualifiedName, sig := range r.signatures {
		if strings.HasSuffix(qualifiedName, "."+name) || qualifiedName == name {
			return sig, true
		}
	}
	return ComponentSignature{}, false
}

// AddLocalTemplateAlias adds an alias for a local template
func (r *SymbolResolver) AddLocalTemplateAlias(alias, target string) {
	// Find the target signature
	for qualifiedName, sig := range r.signatures {
		if strings.HasSuffix(qualifiedName, "."+target) {
			// Create alias with same package path
			parts := strings.Split(qualifiedName, ".")
			if len(parts) >= 2 {
				packagePath := strings.Join(parts[:len(parts)-1], ".")
				aliasQualifiedName := packagePath + "." + alias
				r.signatures[aliasQualifiedName] = sig
			}
			break
		}
	}
}

// GetAllLocalTemplateNames returns all local template names for debugging
func (r *SymbolResolver) GetAllLocalTemplateNames() []string {
	var names []string
	for qualifiedName := range r.signatures {
		// Extract just the component name from the qualified name
		parts := strings.Split(qualifiedName, ".")
		if len(parts) >= 2 {
			names = append(names, parts[len(parts)-1])
		}
	}
	return names
}

// ResolveExpression resolves an expression with context awareness
func (r *SymbolResolver) ResolveExpression(expr string, ctx GeneratorContext, fromDir string) (*TypeInfo, error) {
	expr = strings.TrimSpace(expr)
	
	// First check local scopes (for loops, if statements, etc.)
	for i := len(ctx.LocalScopes) - 1; i >= 0; i-- {
		if typeInfo, ok := ctx.LocalScopes[i].Variables[expr]; ok {
			return typeInfo, nil
		}
	}
	
	// Then check template parameters if we're in a template
	if ctx.CurrentTemplate != nil {
		// Load the current package to get proper type info
		pkg, err := r.ensurePackageLoaded(fromDir)
		if err == nil {
			tmplName := getTemplateName(ctx.CurrentTemplate)
			// Look up the template function in the package
			obj := pkg.Types.Scope().Lookup(tmplName)
			if obj != nil {
				if fn, ok := obj.(*types.Func); ok {
					sig := fn.Type().(*types.Signature)
					params := sig.Params()
					for i := 0; i < params.Len(); i++ {
						param := params.At(i)
						if param.Name() == expr {
							paramInfo := r.analyzeParameterType(param.Name(), param.Type())
							return &TypeInfo{
								FullType:     paramInfo.Type,
								IsComponent:  paramInfo.IsComponent,
								IsAttributer: paramInfo.IsAttributer,
								IsPointer:    paramInfo.IsPointer,
								IsSlice:      paramInfo.IsSlice,
								IsMap:        paramInfo.IsMap,
								IsString:     paramInfo.IsString,
								IsBool:       paramInfo.IsBool,
							}, nil
						}
					}
				}
			}
		}
	}
	
	// Try package-level symbol resolution
	pkg, err := r.ensurePackageLoaded(fromDir)
	if err == nil {
		obj := pkg.Types.Scope().Lookup(expr)
		if obj != nil {
			typeInfo := r.analyzeParameterType(obj.Name(), obj.Type())
			return &TypeInfo{
				FullType:     typeInfo.Type,
				IsComponent:  typeInfo.IsComponent,
				IsAttributer: typeInfo.IsAttributer,
				IsPointer:    typeInfo.IsPointer,
				IsSlice:      typeInfo.IsSlice,
				IsMap:        typeInfo.IsMap,
				IsString:     typeInfo.IsString,
				IsBool:       typeInfo.IsBool,
			}, nil
		}
	}
	
	return nil, fmt.Errorf("symbol %s not found in current context", expr)
}

// AddComponentSignature adds a resolved component signature for code generation
func (r *SymbolResolver) AddComponentSignature(sig ComponentSignature) {
	r.signatures[sig.QualifiedName] = sig
}

// GetComponentSignature returns a component signature by qualified name
func (r *SymbolResolver) GetComponentSignature(qualifiedName string) (ComponentSignature, bool) {
	sig, ok := r.signatures[qualifiedName]
	return sig, ok
}

// ClearCache clears the internal cache (useful for testing or memory management)
func (r *SymbolResolver) ClearCache() {
	r.signatures = make(map[string]ComponentSignature)
	r.overlay = make(map[string][]byte)
}

// CacheSize returns the number of cached signatures (useful for monitoring)
func (r *SymbolResolver) CacheSize() int {
	return len(r.signatures)
}

// addLocalTemplate adds a local template signature to cache
func (r *SymbolResolver) addLocalTemplate(sig ComponentSignature) {
	r.signatures[sig.QualifiedName] = sig
}

// extractHTMLTemplateSignature extracts the signature from an HTML template
func (r *SymbolResolver) extractHTMLTemplateSignature(tmpl *parser.HTMLTemplate) (ComponentSignature, bool) {
	// Parse the template declaration from Expression.Value using Go AST parser
	exprValue := tmpl.Expression.Value
	if exprValue == "" {
		return ComponentSignature{}, false
	}

	name, params, err := r.parseTemplateSignatureFromAST(exprValue)
	if err != nil || name == "" {
		return ComponentSignature{}, false
	}

	return ComponentSignature{
		PackagePath: "", // Local package
		Name:        name,
		Parameters:  params,
	}, true
}

// parseTemplateSignatureFromAST parses a templ template signature using Go AST parser
func (r *SymbolResolver) parseTemplateSignatureFromAST(exprValue string) (name string, params []ParameterInfo, err error) {
	// Add "func " prefix to make it a valid Go function declaration for parsing
	funcDecl := "func " + exprValue

	// Create a temporary package to parse the function
	src := "package main\n" + funcDecl

	// Parse the source
	fset := token.NewFileSet()
	node, parseErr := goparser.ParseFile(fset, "", src, goparser.AllErrors)
	if parseErr != nil || node == nil {
		return "", nil, parseErr
	}

	// Extract function declaration from AST
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			name = fn.Name.Name
			params = r.extractParametersFromAST(fn.Type.Params)

			// If this is a receiver method, create a composite name
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				receiverType := r.astTypeToString(fn.Recv.List[0].Type)
				// Remove pointer indicator if present for consistent naming
				receiverType = strings.TrimPrefix(receiverType, "*")
				name = receiverType + "." + name
			}

			return name, params, nil
		}
	}

	return "", nil, nil
}

// extractParametersFromAST extracts parameter information from AST field list with type analysis
func (r *SymbolResolver) extractParametersFromAST(fieldList *ast.FieldList) []ParameterInfo {
	if fieldList == nil || len(fieldList.List) == 0 {
		return nil
	}

	var params []ParameterInfo

	for _, field := range fieldList.List {
		fieldType := r.astTypeToString(field.Type)

		// Analyze type characteristics from AST
		typeInfo := r.analyzeASTType(fieldType, field.Type)

		// Handle multiple names with the same type (e.g., "a, b string")
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				param := typeInfo
				param.Name = name.Name
				params = append(params, param)
			}
		} else {
			// Handle anonymous parameters
			param := typeInfo
			param.Name = ""
			params = append(params, param)
		}
	}

	return params
}

// analyzeASTType analyzes type characteristics from AST (for templ templates)
func (r *SymbolResolver) analyzeASTType(typeStr string, expr ast.Expr) ParameterInfo {
	// For templ templates, we do basic analysis based on AST structure
	// This is less comprehensive than full type resolution but sufficient for templates

	isComponent := (typeStr == "templ.Component" || typeStr == "github.com/a-h/templ.Component")
	isAttributer := (typeStr == "templ.Attributer" || typeStr == "github.com/a-h/templ.Attributer")
	isPointer := false
	isSlice := false
	isMap := false
	isString := false
	isBool := false

	// Analyze AST structure
	switch t := expr.(type) {
	case *ast.Ident:
		// Basic types: string, int, bool, etc.
		isString = t.Name == "string"
		isBool = t.Name == "bool"
	case *ast.StarExpr:
		// Pointer types
		isPointer = true
		if ident, ok := t.X.(*ast.Ident); ok {
			isString = ident.Name == "string"
			isBool = ident.Name == "bool"
		}
	case *ast.ArrayType:
		// Slice or array types
		isSlice = true
	case *ast.MapType:
		// Map types
		isMap = true
	case *ast.SelectorExpr:
		// Qualified types like templ.Component
		if typeStr == "templ.Component" || typeStr == "github.com/a-h/templ.Component" {
			isComponent = true
		}
		if typeStr == "templ.Attributer" || typeStr == "github.com/a-h/templ.Attributer" {
			isAttributer = true
		}
	}

	return ParameterInfo{
		Name:         "", // Will be set by caller
		Type:         typeStr,
		IsComponent:  isComponent,
		IsAttributer: isAttributer,
		IsPointer:    isPointer,
		IsSlice:      isSlice,
		IsMap:        isMap,
		IsString:     isString,
		IsBool:       isBool,
	}
}

// astTypeToString converts AST type expressions to their string representation
func (r *SymbolResolver) astTypeToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		// Basic types like string, int, bool, etc.
		return t.Name
	case *ast.StarExpr:
		// Pointer types like *string
		return "*" + r.astTypeToString(t.X)
	case *ast.ArrayType:
		// Array or slice types like []string, [10]int
		if t.Len == nil {
			// Slice
			return "[]" + r.astTypeToString(t.Elt)
		} else {
			// Array
			return "[...]" + r.astTypeToString(t.Elt)
		}
	case *ast.MapType:
		// Map types like map[string]int
		return "map[" + r.astTypeToString(t.Key) + "]" + r.astTypeToString(t.Value)
	case *ast.SelectorExpr:
		// Qualified types like time.Time, context.Context
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.InterfaceType:
		return "any"
	default:
		return ""
	}
}

// extractGoTypeSignatures extracts type definitions from Go code that might implement Component
func (r *SymbolResolver) extractGoTypeSignatures(goExpr *parser.TemplateFileGoExpression, packagePath string) {
	// Parse the Go code
	src := "package main\n" + goExpr.Expression.Value
	fset := token.NewFileSet()
	node, err := goparser.ParseFile(fset, "", src, goparser.AllErrors)
	if err != nil || node == nil {
		return
	}

	// Look for type declarations and methods
	typeNames := make(map[string]bool)

	// First pass: collect all type names
	for _, decl := range node.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.TYPE {
			for _, spec := range genDecl.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					typeNames[typeSpec.Name.Name] = true
				}
			}
		}
	}

	// Second pass: look for Render methods on these types
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "Render" && fn.Recv != nil {
			// Check if this is a method on one of our types
			if len(fn.Recv.List) > 0 {
				receiverType := r.astTypeToString(fn.Recv.List[0].Type)
				receiverType = strings.TrimPrefix(receiverType, "*")

				if typeNames[receiverType] {
					// Check if the signature matches Component.Render
					if r.isComponentRenderMethod(fn) {
						// Check if receiver is a pointer
						isPointerRecv := strings.HasPrefix(r.astTypeToString(fn.Recv.List[0].Type), "*")

						// This type implements Component
						sig := ComponentSignature{
							PackagePath:   packagePath,
							Name:          receiverType,
							QualifiedName: packagePath + "." + receiverType,
							Parameters:    []ParameterInfo{}, // Component types have no parameters
							IsStruct:      true,
							IsPointerRecv: isPointerRecv,
						}
						r.addLocalTemplate(sig)
					}
				}
			}
		}
	}
}

// isComponentRenderMethod checks if a function declaration matches the Component.Render signature
func (r *SymbolResolver) isComponentRenderMethod(fn *ast.FuncDecl) bool {
	// Check parameters: (ctx context.Context, w io.Writer)
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 2 {
		return false
	}

	// Check return type: error
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}

	// Check if return type is error
	if retType, ok := fn.Type.Results.List[0].Type.(*ast.Ident); !ok || retType.Name != "error" {
		return false
	}

	return true
}

// implementsComponent checks if a type implements the templ.Component interface using full type information
// Returns (implements, isPointerReceiver)
func (r *SymbolResolver) implementsComponent(t types.Type, pkg *types.Package) (bool, bool) {
	method, _, _ := types.LookupFieldOrMethod(t, true, pkg, "Render")
	if method == nil {
		return false, false
	}

	fn, ok := method.(*types.Func)
	if !ok {
		return false, false
	}

	sig := fn.Type().(*types.Signature)

	// Check parameters: (context.Context, io.Writer)
	params := sig.Params()
	if params.Len() != 2 {
		return false, false
	}
	if params.At(0).Type().String() != "context.Context" {
		return false, false
	}
	if params.At(1).Type().String() != "io.Writer" {
		return false, false
	}

	// Check return type: error
	results := sig.Results()
	if results.Len() != 1 {
		return false, false
	}
	returnType := results.At(0).Type().String()
	if returnType != "error" {
		return false, false
	}

	// Check if the receiver is a pointer by examining the method signature
	isPointerReceiver := false
	if sig.Recv() != nil {
		recvType := sig.Recv().Type()
		_, isPointerReceiver = recvType.(*types.Pointer)
	}

	return true, isPointerReceiver
}

// analyzeParameterType performs comprehensive type analysis for a parameter
func (r *SymbolResolver) analyzeParameterType(name string, t types.Type) ParameterInfo {
	typeStr := t.String()

	// Analyze the type for various characteristics
	isComponent := r.implementsComponentInterface(t)
	isAttributer := r.implementsAttributerInterface(t)
	isPointer := false
	isSlice := false
	isMap := false
	isString := false
	isBool := false

	// Check underlying type characteristics
	switch underlying := t.Underlying().(type) {
	case *types.Pointer:
		isPointer = true
		// Check what the pointer points to
		pointee := underlying.Elem().Underlying()
		switch pointee.(type) {
		case *types.Basic:
			basic := pointee.(*types.Basic)
			isString = basic.Kind() == types.String
			isBool = basic.Kind() == types.Bool
		}
	case *types.Slice:
		isSlice = true
	case *types.Map:
		isMap = true
	case *types.Basic:
		isString = underlying.Kind() == types.String
		isBool = underlying.Kind() == types.Bool
	}

	// Also check named types (e.g., type MyString string)
	if named, ok := t.(*types.Named); ok {
		if basic, ok := named.Underlying().(*types.Basic); ok {
			isString = basic.Kind() == types.String
			isBool = basic.Kind() == types.Bool
		}
	}

	// Fallback: If we have an invalid type but the parameter is named "attrs",
	// assume it's a templ.Attributer (common pattern in templ)
	if typeStr == "invalid type" && name == "attrs" {
		isAttributer = true
	}
	
	return ParameterInfo{
		Name:         name,
		Type:         typeStr,
		IsComponent:  isComponent,
		IsAttributer: isAttributer,
		IsPointer:    isPointer,
		IsSlice:      isSlice,
		IsMap:        isMap,
		IsString:     isString,
		IsBool:       isBool,
	}
}

// implementsComponentInterface checks if a type implements templ.Component interface
func (r *SymbolResolver) implementsComponentInterface(t types.Type) bool {
	// Look for Render(context.Context, io.Writer) error method
	method, _, _ := types.LookupFieldOrMethod(t, true, nil, "Render")
	if method == nil {
		return false
	}

	fn, ok := method.(*types.Func)
	if !ok {
		return false
	}

	sig := fn.Type().(*types.Signature)

	// Check parameters: (context.Context, io.Writer)
	params := sig.Params()
	if params.Len() != 2 {
		return false
	}
	if params.At(0).Type().String() != "context.Context" {
		return false
	}
	if params.At(1).Type().String() != "io.Writer" {
		return false
	}

	// Check return type: error
	results := sig.Results()
	if results.Len() != 1 {
		return false
	}
	if results.At(0).Type().String() != "error" {
		return false
	}

	return true
}

// implementsAttributerInterface checks if a type implements templ.Attributer interface
func (r *SymbolResolver) implementsAttributerInterface(t types.Type) bool {
	// Check for both qualified and unqualified templ.Attributer
	typeStr := t.String()
	return typeStr == "templ.Attributer" || typeStr == "github.com/a-h/templ.Attributer"
}

// extractStructFieldsWithTypeAnalysis extracts exported struct fields with rich type analysis
func (r *SymbolResolver) extractStructFieldsWithTypeAnalysis(t types.Type) []ParameterInfo {
	var structType *types.Struct
	switch underlying := t.Underlying().(type) {
	case *types.Struct:
		structType = underlying
	default:
		return nil
	}

	var fields []ParameterInfo
	for i := 0; i < structType.NumFields(); i++ {
		field := structType.Field(i)
		if field.Exported() {
			fields = append(fields, r.analyzeParameterType(field.Name(), field.Type()))
		}
	}

	return fields
}

// extractStructFields extracts exported struct fields for struct components (legacy compatibility)
func (r *SymbolResolver) extractStructFields(t types.Type) []ParameterInfo {
	return r.extractStructFieldsWithTypeAnalysis(t)
}

// getTemplateName extracts the template name from an HTMLTemplate node
func getTemplateName(tmpl *parser.HTMLTemplate) string {
	if tmpl == nil {
		return ""
	}
	// Extract template name from the expression value
	// The expression value is like "Container(child templ.Component)"
	// or "(StructComponent) Page(title string)"
	exprValue := strings.TrimSpace(tmpl.Expression.Value)

	// Check if this is a struct method template
	if strings.HasPrefix(exprValue, "(") {
		// Find the closing parenthesis for the receiver
		if idx := strings.Index(exprValue, ")"); idx != -1 {
			// Extract the method name after the receiver
			methodPart := strings.TrimSpace(exprValue[idx+1:])
			if methodIdx := strings.Index(methodPart, "("); methodIdx != -1 {
				return strings.TrimSpace(methodPart[:methodIdx])
			}
			return methodPart
		}
	}

	// Regular template: extract name before the first parenthesis
	if idx := strings.Index(exprValue, "("); idx != -1 {
		return strings.TrimSpace(exprValue[:idx])
	}
	return exprValue
}

// extractStructMethodInfo extracts receiver type and method name from a struct method template
func extractStructMethodInfo(tmpl *parser.HTMLTemplate) (receiver string, methodName string) {
	if tmpl == nil {
		return "", ""
	}

	exprValue := strings.TrimSpace(tmpl.Expression.Value)

	// Check if this is a struct method template: (ReceiverType) MethodName(...)
	if !strings.HasPrefix(exprValue, "(") {
		return "", ""
	}

	// Find the closing parenthesis for the receiver
	idx := strings.Index(exprValue, ")")
	if idx == -1 {
		return "", ""
	}

	// Extract receiver (without parentheses)
	receiverPart := strings.TrimSpace(exprValue[1:idx])
	// Remove pointer if present
	receiverPart = strings.TrimPrefix(receiverPart, "*")
	receiverPart = strings.TrimSpace(receiverPart)

	// Extract method name
	methodPart := strings.TrimSpace(exprValue[idx+1:])
	if methodIdx := strings.Index(methodPart, "("); methodIdx != -1 {
		methodName = strings.TrimSpace(methodPart[:methodIdx])
	} else {
		methodName = methodPart
	}

	return receiverPart, methodName
}

// extractTemplateParams extracts parameters from a template expression
func extractTemplateParams(expr parser.Expression) []ParameterInfo {
	var params []ParameterInfo

	// Parse the parameters from the expression
	// Format: "TemplateName(param1 type1, param2 type2)" or "(Receiver) MethodName(param1 type1)"
	exprValue := strings.TrimSpace(expr.Value)

	// Skip receiver part if this is a struct method
	if strings.HasPrefix(exprValue, "(") {
		if idx := strings.Index(exprValue, ")"); idx != -1 {
			// Use the part after the receiver
			exprValue = strings.TrimSpace(exprValue[idx+1:])
		}
	}

	if idx := strings.Index(exprValue, "("); idx != -1 {
		if endIdx := strings.LastIndex(exprValue, ")"); endIdx != -1 {
			paramsStr := exprValue[idx+1 : endIdx]
			if paramsStr != "" {
				// Split by comma
				paramParts := strings.Split(paramsStr, ",")
				for _, part := range paramParts {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}

					// Find the last space to separate name from type
					lastSpace := strings.LastIndex(part, " ")
					if lastSpace != -1 {
						name := strings.TrimSpace(part[:lastSpace])
						typeStr := strings.TrimSpace(part[lastSpace+1:])
						params = append(params, ParameterInfo{
							Name: name,
							Type: typeStr,
						})
					}
				}
			}
		}
	}

	return params
}

// analyzeParameterTypeString analyzes a parameter type from its string representation
func (r *SymbolResolver) analyzeParameterTypeString(param *ParameterInfo) {
	typeStr := param.Type

	// Basic type analysis
	param.IsPointer = strings.HasPrefix(typeStr, "*")
	param.IsSlice = strings.HasPrefix(typeStr, "[]")
	param.IsMap = strings.HasPrefix(typeStr, "map[")
	param.IsString = typeStr == "string"
	param.IsBool = typeStr == "bool"

	// Check for known interfaces
	if strings.HasSuffix(typeStr, "templ.Component") || typeStr == "templ.Component" {
		param.IsComponent = true
	}

	if strings.HasSuffix(typeStr, "templ.Attributer") || typeStr == "templ.Attributer" {
		param.IsAttributer = true
	}
}

// GeneratorContext tracks position in AST during code generation
type GeneratorContext struct {
	CurrentTemplate *parser.HTMLTemplate // Current template we're generating
	ASTPath         []parser.Node        // Path from root to current node
	LocalScopes     []LocalScope         // Stack of local scopes
}

// LocalScope represents a scope created by an AST node
type LocalScope struct {
	Node      parser.Node          // The AST node that created this scope
	Variables map[string]*TypeInfo // Variables defined in this scope
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

// getPackagePathFromDir determines the package path from a directory
func (r *SymbolResolver) getPackagePathFromDir(dir string) (string, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedModule,
		Dir:  dir,
	}
	
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return "", err
	}
	
	if len(pkgs) == 0 {
		return "", fmt.Errorf("no package found in directory %s", dir)
	}
	
	return pkgs[0].PkgPath, nil
}
