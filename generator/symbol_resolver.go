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
	signatures     map[string]ComponentSignature // Cache keyed by fully qualified names
	overlay        map[string][]byte             // Go file overlays for templ templates
	packageCache   map[string]*packages.Package  // Cache of loaded packages by directory
	currentPackage *packages.Package             // Current package being processed
	currentPkgPath string                        // Current package path
}

// newSymbolResolver creates a new symbol resolver
func newSymbolResolver() SymbolResolver {
	return SymbolResolver{
		signatures:   make(map[string]ComponentSignature),
		overlay:      make(map[string][]byte),
		packageCache: make(map[string]*packages.Package),
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
		sig, err = r.ResolveComponent(fromDir, packagePath, localName)
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

	sig, err := r.ResolveComponent(fromDir, currentPkg, componentName)
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
	sig, err := r.extractComponentSignature(methodObj, pkg.PkgPath)
	if err != nil {
		return ComponentSignature{}, false
	}

	// Create alias for future lookups
	sig.QualifiedName = componentName
	r.signatures[componentName] = sig
	return sig, true
}

// ResolveComponent resolves a component starting from a specific directory
// This is the primary method used during code generation
func (r *SymbolResolver) ResolveComponent(fromDir, pkgPath, componentName string) (ComponentSignature, error) {
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
		return ComponentSignature{}, fmt.Errorf("failed to load package %s: %w", pkgPath, err)
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
			return ComponentSignature{}, fmt.Errorf("package %s has non-generated file errors: %v", pkgPath, pkg.Errors)
		}
	}

	// Look for the component in the package's type information
	if pkg.Types == nil {
		return ComponentSignature{}, fmt.Errorf("no type information available for package %s", pkgPath)
	}

	obj := pkg.Types.Scope().Lookup(componentName)
	if obj == nil {
		return ComponentSignature{}, fmt.Errorf("component %s not found in package %s", componentName, pkgPath)
	}

	// Extract signature using the sophisticated logic from SymbolResolver
	sig, err := r.extractComponentSignature(obj, pkgPath)
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

// generateOverlay creates Go stub code for templ templates
func (r *SymbolResolver) generateOverlay(tf *parser.TemplateFile, pkgName string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("package %s\n\n", pkgName))

	// First pass: collect imports and write top-level Go code
	hasImports := false
	hasTemplImport := false
	for _, node := range tf.Nodes {
		if goExpr, ok := node.(*parser.TemplateFileGoExpression); ok {
			// This includes imports, type definitions, variables, etc.
			exprValue := goExpr.Expression.Value

			// Check if this is an import block
			if strings.Contains(exprValue, "import") {
				hasImports = true

				// Check if templ is already imported
				if strings.Contains(exprValue, "github.com/a-h/templ") && !strings.Contains(exprValue, "templ/generator") {
					hasTemplImport = true
				}

				// If this is an import block and templ is not imported, add it
				if strings.Contains(exprValue, "import (") && !hasTemplImport {
					// Insert templ import into the existing import block
					insertPos := strings.Index(exprValue, "import (") + len("import (")
					exprValue = exprValue[:insertPos] + "\n\t\"github.com/a-h/templ\"" + exprValue[insertPos:]
					hasTemplImport = true
				}
			}

			sb.WriteString(exprValue)
			sb.WriteString("\n\n")
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
			// Use the parsed AST if available, otherwise fall back to parsing
			var name string
			funcDecl, ok := n.Expression.Stmt.(*ast.FuncDecl)
			if !ok {
				// Skip templates without valid function declarations
				continue
			}
			name = funcDecl.Name.Name
			// If this is a receiver method, create a composite name
			if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
				receiverType := r.astTypeToString(funcDecl.Recv.List[0].Type)
				// Remove pointer indicator if present for consistent naming
				receiverType = strings.TrimPrefix(receiverType, "*")
				name = receiverType + "." + name
			}

			// Generate proper function signature
			sb.WriteString("func ")
			sb.WriteString(n.Expression.Value)
			sb.WriteString(" templ.Component {\n")
			sb.WriteString("\treturn templ.NopComponent\n")
			sb.WriteString("}\n\n")

		case *parser.CSSTemplate:
			// CSS templates become functions returning templ.CSSClass
			sb.WriteString("func ")
			sb.WriteString(n.Name)
			sb.WriteString("() templ.CSSClass {\n")
			sb.WriteString("\treturn templ.ComponentCSSClass{}\n")
			sb.WriteString("}\n\n")

		// TODO: Script templates are deprecated?
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

// extractComponentSignature extracts component signature
func (r *SymbolResolver) extractComponentSignature(obj types.Object, pkgPath string) (ComponentSignature, error) {
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
			return ComponentSignature{}, fmt.Errorf("%s is neither a function nor a type", obj.Name())
		}

		isStruct, isPointerRecv = r.implementsComponent(typeName.Type(), typeName.Pkg())
		if !isStruct {
			return ComponentSignature{}, fmt.Errorf("%s does not implement templ.Component interface", obj.Name())
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
		switch pointee := pointee.(type) {
		case *types.Basic:
			basic := pointee
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

	// Special handling for templ.Component type
	if typeStr == "templ.Component" || typeStr == "github.com/a-h/templ.Component" {
		isComponent = true
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

// extractForLoopVariables extracts variables from a for expression using the AST
// e.g., "for i, item := range items" -> ["i", "item"]
func extractForLoopVariables(expr parser.Expression) map[string]*TypeInfo {
	vars := make(map[string]*TypeInfo)

	// Check if we have an AST node
	if expr.Stmt == nil {
		// Fallback to simple heuristics if no AST
		return extractForLoopVariablesFallback(expr.Value)
	}

	switch stmt := expr.Stmt.(type) {
	case *ast.RangeStmt:
		// Handle "for key, value := range expr" pattern
		if stmt.Key != nil {
			if ident, ok := stmt.Key.(*ast.Ident); ok && ident.Name != "_" {
				// Key is usually int for slices/arrays, string for maps
				vars[ident.Name] = &TypeInfo{
					FullType: "int", // Simplified - would need type analysis for maps
				}
			}
		}
		if stmt.Value != nil {
			if ident, ok := stmt.Value.(*ast.Ident); ok && ident.Name != "_" {
				// Value type is unknown without more context
				vars[ident.Name] = &TypeInfo{
					FullType: "interface{}",
				}
			}
		}

	case *ast.ForStmt:
		// Handle "for i := 0; i < n; i++" pattern
		if stmt.Init != nil {
			if assignStmt, ok := stmt.Init.(*ast.AssignStmt); ok {
				for _, lhs := range assignStmt.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok {
						vars[ident.Name] = &TypeInfo{
							FullType: "int", // Common case for loop counters
						}
					}
				}
			}
		}
	}

	return vars
}

// extractForLoopVariablesFallback is the fallback implementation using string parsing
func extractForLoopVariablesFallback(exprValue string) map[string]*TypeInfo {
	vars := make(map[string]*TypeInfo)
	exprStr := strings.TrimSpace(exprValue)

	// Handle "for i, v := range expr" pattern
	if strings.Contains(exprStr, ":=") && strings.Contains(exprStr, "range") {
		parts := strings.Split(exprStr, ":=")
		if len(parts) >= 2 {
			varPart := strings.TrimSpace(parts[0])
			varNames := strings.Split(varPart, ",")

			for i, varName := range varNames {
				varName = strings.TrimSpace(varName)
				if varName != "" && varName != "_" {
					if i == 0 {
						vars[varName] = &TypeInfo{
							FullType: "int",
						}
					} else {
						vars[varName] = &TypeInfo{
							FullType: "interface{}",
						}
					}
				}
			}
		}
	}

	// Handle "for i := 0; i < n; i++" pattern
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

// extractIfConditionVariables extracts variables from if condition using the AST
// e.g., "if err := doSomething(); err != nil" -> ["err"]
func extractIfConditionVariables(expr parser.Expression) map[string]*TypeInfo {
	vars := make(map[string]*TypeInfo)

	// Check if we have an AST node
	if expr.Stmt == nil {
		// Fallback to simple heuristics if no AST
		return extractIfConditionVariablesFallback(expr.Value)
	}

	if ifStmt, ok := expr.Stmt.(*ast.IfStmt); ok {
		// Check if there's an Init statement (e.g., "if x := expr; condition")
		if ifStmt.Init != nil {
			if assignStmt, ok := ifStmt.Init.(*ast.AssignStmt); ok {
				for _, lhs := range assignStmt.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok && ident.Name != "_" {
						// Without type analysis, we can't know the exact type
						vars[ident.Name] = &TypeInfo{
							FullType: "interface{}",
						}
					}
				}
			}
		}
	}

	return vars
}

// extractIfConditionVariablesFallback is the fallback implementation using string parsing
func extractIfConditionVariablesFallback(exprValue string) map[string]*TypeInfo {
	vars := make(map[string]*TypeInfo)
	exprStr := strings.TrimSpace(exprValue)

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
