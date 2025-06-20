package generator

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/a-h/templ/parser/v2"
	"golang.org/x/tools/go/packages"
)

// componentSignature represents a templ component's function signature or struct fields
type componentSignature struct {
	packagePath   string
	name          string
	qualifiedName string          // For functions: pkgPath.Name, For structs: pkgPath.TypeName
	parameters    []parameterInfo // For functions: parameters, For structs: exported fields
	isStruct      bool
	isPointerRecv bool
}

// parameterInfo represents a function parameter or struct field with rich type information
type parameterInfo struct {
	name         string
	typ          string
	isComponent  bool
	isAttributer bool
	isPointer    bool
	isSlice      bool
	isMap        bool
	isString     bool
	isBool       bool
}

// symbolTypeInfo contains comprehensive type information
type symbolTypeInfo struct {
	fullType     string // e.g., "templ.Component"
	isPointer    bool
	isComponent  bool
	isAttributer bool
	isString     bool
	isBool       bool
	isSlice      bool
	isMap        bool
}

// symbolResolver automatically detects module roots and provides unified resolution
// for both templ templates and Go components across packages
type symbolResolver struct {
	signatures   map[string]componentSignature // Cache keyed by fully qualified names
	overlay      map[string][]byte             // Go file overlays for templ templates
	packageCache map[string]*packages.Package  // Cache of loaded packages by directory
}

// newSymbolResolver creates a new symbol resolver
func newSymbolResolver() symbolResolver {
	return symbolResolver{
		signatures:   make(map[string]componentSignature),
		overlay:      make(map[string][]byte),
		packageCache: make(map[string]*packages.Package),
	}
}

// resolveElementComponent resolves a component for element syntax during code generation
// This is the main entry point for element component resolution
func (r *symbolResolver) resolveElementComponent(fromDir, currentPkg string, componentName string, tf *parser.TemplateFile) (componentSignature, error) {
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

	var sig componentSignature
	var err error

	if packagePath != "" {
		// Cross-package resolution
		sig, err = r.resolveComponent(fromDir, packagePath, localName)
	} else {
		// Local resolution - try multiple strategies
		sig, err = r.resolveLocalComponent(fromDir, currentPkg, localName, tf)
	}

	if err != nil {
		return componentSignature{}, err
	}

	// Cache with original component name for future lookups
	sig.qualifiedName = componentName
	r.signatures[componentName] = sig

	return sig, nil
}

// resolveImportAlias resolves an import alias to its full package path
func (r *symbolResolver) resolveImportAlias(alias string, tf *parser.TemplateFile) string {
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
func (r *symbolResolver) parseImportPath(goCode, packageAlias string) string {
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
func (r *symbolResolver) resolveLocalComponent(fromDir, currentPkg, componentName string, tf *parser.TemplateFile) (componentSignature, error) {
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
			return componentSignature{}, fmt.Errorf("failed to determine package path: %w", err)
		}
	}

	sig, err := r.resolveComponent(fromDir, currentPkg, componentName)
	if err == nil {
		return sig, nil
	}

	return componentSignature{}, fmt.Errorf("component %s not found: %w", componentName, err)
}

// resolveStructMethod resolves struct variable method components like structVar.Method
func (r *symbolResolver) resolveStructMethod(componentName string, tf *parser.TemplateFile, fromDir string) (componentSignature, bool) {
	parts := strings.Split(componentName, ".")
	if len(parts) < 2 {
		return componentSignature{}, false
	}

	varName := parts[0]
	methodName := strings.Join(parts[1:], ".")

	// Ensure package is loaded with overlays
	pkg, err := r.ensurePackageLoaded(fromDir, "")
	if err != nil {
		return componentSignature{}, false
	}

	// Look for the variable in the package scope
	obj := pkg.Types.Scope().Lookup(varName)
	if obj == nil {
		return componentSignature{}, false
	}

	// Get the type of the variable
	varType := obj.Type()

	// If it's a pointer, get the element type
	if ptr, ok := varType.(*types.Pointer); ok {
		varType = ptr.Elem()
	}

	// Check if it's a named type
	if _, ok := varType.(*types.Named); !ok {
		return componentSignature{}, false
	}

	// Look for the method
	methodObj, _, _ := types.LookupFieldOrMethod(varType, true, pkg.Types, methodName)
	if methodObj == nil {
		return componentSignature{}, false
	}

	// Extract signature
	sig, err := r.extractComponentSignature(methodObj, pkg.PkgPath)
	if err != nil {
		return componentSignature{}, false
	}

	// Create alias for future lookups
	sig.qualifiedName = componentName
	r.signatures[componentName] = sig
	return sig, true
}

// resolveComponent resolves a component starting from a specific directory
// This is the primary method used during code generation
func (r *symbolResolver) resolveComponent(fromDir, pkgPath, componentName string) (componentSignature, error) {
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

	// Use ensurePackageLoaded which properly handles overlays
	pkg, err := r.ensurePackageLoaded(fromDir, pkgPath)
	if err != nil {
		return componentSignature{}, fmt.Errorf("failed to load package %s: %w", pkgPath, err)
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
			return componentSignature{}, fmt.Errorf("package %s has non-generated file errors: %v", pkgPath, pkg.Errors)
		}
	}

	// Look for the component in the package's type information
	if pkg.Types == nil {
		return componentSignature{}, fmt.Errorf("no type information available for package %s", pkgPath)
	}

	obj := pkg.Types.Scope().Lookup(componentName)
	if obj == nil {
		return componentSignature{}, fmt.Errorf("component %s not found in package %s", componentName, pkgPath)
	}

	// Extract signature using the sophisticated logic from SymbolResolver
	sig, err := r.extractComponentSignature(obj, pkgPath)
	if err != nil {
		return componentSignature{}, err
	}

	// Set the fully qualified name for caching
	if qualifiedName == "" {
		// For local components, use the actual package path we resolved
		qualifiedName = pkg.PkgPath + "." + componentName
		sig.qualifiedName = qualifiedName
	} else {
		sig.qualifiedName = qualifiedName
	}

	// Cache using fully qualified name
	r.signatures[qualifiedName] = sig
	return sig, nil
}

// generateOverlay creates Go stub code for templ templates
func (r *symbolResolver) generateOverlay(tf *parser.TemplateFile, pkgName string) string {
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
			if _, ok := n.Expression.Stmt.(*ast.FuncDecl); !ok {
				// Skip templates without valid function declarations
				continue
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
func (r *symbolResolver) extractComponentSignature(obj types.Object, pkgPath string) (componentSignature, error) {
	var paramInfo []parameterInfo
	var isStruct, isPointerRecv bool

	// The component can be either a function or a type that implements templ.Component
	if fn, ok := obj.(*types.Func); ok {
		sig := fn.Type().(*types.Signature)
		params := sig.Params()
		paramInfo = make([]parameterInfo, 0, params.Len())

		for i := 0; i < params.Len(); i++ {
			param := params.At(i)
			paramInfo = append(paramInfo, r.analyzeParameterType(param.Name(), param.Type()))
		}
	} else {
		typeName, ok := obj.(*types.TypeName)
		if !ok {
			return componentSignature{}, fmt.Errorf("%s is neither a function nor a type", obj.Name())
		}

		isStruct, isPointerRecv = r.implementsComponent(typeName.Type(), typeName.Pkg())
		if !isStruct {
			return componentSignature{}, fmt.Errorf("%s does not implement templ.Component interface", obj.Name())
		}

		// Extract struct fields for struct components
		paramInfo = r.extractStructFieldsWithTypeAnalysis(typeName.Type())
	}

	return componentSignature{
		packagePath:   pkgPath,
		name:          obj.Name(),
		parameters:    paramInfo,
		isStruct:      isStruct,
		isPointerRecv: isPointerRecv,
	}, nil
}

// registerTemplOverlay registers a template file for symbol resolution by creating a Go overlay
// This method generates an overlay that makes the templ file available to the Go package loader
func (r *symbolResolver) registerTemplOverlay(tf *parser.TemplateFile, fileName string) error {
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

	// Check if already registered
	if _, exists := r.overlay[overlayPath]; exists {
		return nil // Already registered
	}

	// Extract package name from the template file
	pkgName := ""
	if tf.Package.Expression.Value != "" {
		pkgName = strings.TrimPrefix(tf.Package.Expression.Value, "package ")
		pkgName = strings.TrimSpace(pkgName)
	}

	if pkgName == "" {
		return fmt.Errorf("no package declaration found in template file")
	}

	overlayContent := r.generateOverlay(tf, pkgName)
	r.overlay[overlayPath] = []byte(overlayContent)

	return nil
}

// ensurePackageLoaded lazily loads the package with full type information
// It can load either by directory (pattern ".") or by package path
func (r *symbolResolver) ensurePackageLoaded(fromDir string, pattern string) (*packages.Package, error) {
	// Determine the pattern to use
	if pattern == "" {
		pattern = "."
	}

	// For package paths, we use the package path as the cache key
	// For local packages, we use the directory
	cacheKey := fromDir
	if pattern != "." {
		cacheKey = pattern
	}

	// Check cache first
	if pkg, ok := r.packageCache[cacheKey]; ok {
		return pkg, nil
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedImports |
			packages.NeedTypes,
		Dir:     fromDir,
		Overlay: r.overlay,
	}

	pkgs, err := packages.Load(cfg, pattern)
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

	r.packageCache[cacheKey] = pkg
	return pkg, nil
}

// getLocalTemplate returns a local template signature by name
// This method is for backward compatibility - prefer using fully qualified names
func (r *symbolResolver) getLocalTemplate(name string) (componentSignature, bool) {
	// Check signatures cache
	for qualifiedName, sig := range r.signatures {
		if strings.HasSuffix(qualifiedName, "."+name) || qualifiedName == name {
			return sig, true
		}
	}
	return componentSignature{}, false
}

// resolveExpression resolves an expression with context awareness
func (r *symbolResolver) resolveExpression(expr string, ctx *generatorContext, fromDir string) (*symbolTypeInfo, error) {
	expr = strings.TrimSpace(expr)

	// First check local scopes (for loops, if statements, etc.)
	for i := len(ctx.localScopes) - 1; i >= 0; i-- {
		if typeInfo, ok := ctx.localScopes[i].variables[expr]; ok {
			return typeInfo, nil
		}
	}

	// Then check template parameters if we're in a template
	if ctx.currentTemplate != nil {
		// Load the current package to get proper type info
		pkg, err := r.ensurePackageLoaded(fromDir, "")
		if err == nil {
			tmplName := getTemplateName(ctx.currentTemplate)
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
							return &symbolTypeInfo{
								fullType:     paramInfo.typ,
								isComponent:  paramInfo.isComponent,
								isAttributer: paramInfo.isAttributer,
								isPointer:    paramInfo.isPointer,
								isSlice:      paramInfo.isSlice,
								isMap:        paramInfo.isMap,
								isString:     paramInfo.isString,
								isBool:       paramInfo.isBool,
							}, nil
						}
					}
				}
			}
		}
	}

	// Try package-level symbol resolution
	pkg, err := r.ensurePackageLoaded(fromDir, "")
	if err == nil {
		obj := pkg.Types.Scope().Lookup(expr)
		if obj != nil {
			typeInfo := r.analyzeParameterType(obj.Name(), obj.Type())
			return &symbolTypeInfo{
				fullType:     typeInfo.typ,
				isComponent:  typeInfo.isComponent,
				isAttributer: typeInfo.isAttributer,
				isPointer:    typeInfo.isPointer,
				isSlice:      typeInfo.isSlice,
				isMap:        typeInfo.isMap,
				isString:     typeInfo.isString,
				isBool:       typeInfo.isBool,
			}, nil
		}
	}

	return nil, fmt.Errorf("symbol %s not found in current context", expr)
}

// astTypeToString converts AST type expressions to their string representation
func (r *symbolResolver) astTypeToString(expr ast.Expr) string {
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
func (r *symbolResolver) implementsComponent(t types.Type, pkg *types.Package) (bool, bool) {
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
func (r *symbolResolver) analyzeParameterType(name string, t types.Type) parameterInfo {
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

	return parameterInfo{
		name:         name,
		typ:          typeStr,
		isComponent:  isComponent,
		isAttributer: isAttributer,
		isPointer:    isPointer,
		isSlice:      isSlice,
		isMap:        isMap,
		isString:     isString,
		isBool:       isBool,
	}
}

// implementsComponentInterface checks if a type implements templ.Component interface
func (r *symbolResolver) implementsComponentInterface(t types.Type) bool {
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
func (r *symbolResolver) implementsAttributerInterface(t types.Type) bool {
	// Check for both qualified and unqualified templ.Attributer
	typeStr := t.String()
	return typeStr == "templ.Attributer" || typeStr == "github.com/a-h/templ.Attributer"
}

// extractStructFieldsWithTypeAnalysis extracts exported struct fields with rich type analysis
func (r *symbolResolver) extractStructFieldsWithTypeAnalysis(t types.Type) []parameterInfo {
	var structType *types.Struct
	switch underlying := t.Underlying().(type) {
	case *types.Struct:
		structType = underlying
	default:
		return nil
	}

	var fields []parameterInfo
	for i := 0; i < structType.NumFields(); i++ {
		field := structType.Field(i)
		if field.Exported() {
			fields = append(fields, r.analyzeParameterType(field.Name(), field.Type()))
		}
	}

	return fields
}

// getPackagePathFromDir determines the package path from a directory
func (r *symbolResolver) getPackagePathFromDir(dir string) (string, error) {
	// Check if we already have this package loaded in cache
	if pkg, ok := r.packageCache[dir]; ok {
		return pkg.PkgPath, nil
	}

	// Otherwise, do a minimal load just to get the package path
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

// generatorContext tracks position in AST during code generation
type generatorContext struct {
	currentTemplate *parser.HTMLTemplate // Current template we're generating
	localScopes     []localScope         // Stack of local scopes
}

// localScope represents a scope created by an AST node
type localScope struct {
	node      parser.Node                // The AST node that created this scope
	variables map[string]*symbolTypeInfo // Variables defined in this scope
}

// newGeneratorContext creates a new generator context
func newGeneratorContext() *generatorContext {
	return &generatorContext{
		localScopes: []localScope{},
	}
}

// pushScope creates a new scope
func (ctx *generatorContext) pushScope(node parser.Node) {
	scope := localScope{
		node:      node,
		variables: make(map[string]*symbolTypeInfo),
	}
	ctx.localScopes = append(ctx.localScopes, scope)
}

// popScope removes the current scope
func (ctx *generatorContext) popScope() {
	if len(ctx.localScopes) > 0 {
		ctx.localScopes = ctx.localScopes[:len(ctx.localScopes)-1]
	}
}

// addVariable adds a variable to the current scope
func (ctx *generatorContext) addVariable(name string, typeInfo *symbolTypeInfo) {
	if len(ctx.localScopes) > 0 {
		currentScope := &ctx.localScopes[len(ctx.localScopes)-1]
		currentScope.variables[name] = typeInfo
	}
}

// setCurrentTemplate sets the current template being generated
func (ctx *generatorContext) setCurrentTemplate(tmpl *parser.HTMLTemplate) {
	ctx.currentTemplate = tmpl
	// When entering a template, we don't push a new scope here
	// The parameters are added separately by the caller
}

// clearCurrentTemplate clears the current template
func (ctx *generatorContext) clearCurrentTemplate() {
	if ctx.currentTemplate != nil {
		ctx.popScope() // Remove template parameter scope
		ctx.currentTemplate = nil
	}
}

// extractForLoopVariables extracts variables from a for expression using the AST
// e.g., "for i, item := range items" -> ["i", "item"]
func extractForLoopVariables(expr parser.Expression) map[string]*symbolTypeInfo {
	vars := make(map[string]*symbolTypeInfo)

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
				vars[ident.Name] = &symbolTypeInfo{
					fullType: "int", // Simplified - would need type analysis for maps
				}
			}
		}
		if stmt.Value != nil {
			if ident, ok := stmt.Value.(*ast.Ident); ok && ident.Name != "_" {
				// Value type is unknown without more context
				vars[ident.Name] = &symbolTypeInfo{
					fullType: "interface{}", // impossible to determine exact type without analysis
				}
			}
		}

	case *ast.ForStmt:
		// Handle "for i := 0; i < n; i++" pattern
		if stmt.Init != nil {
			if assignStmt, ok := stmt.Init.(*ast.AssignStmt); ok {
				for _, lhs := range assignStmt.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok {
						vars[ident.Name] = &symbolTypeInfo{
							fullType: "int", // Common case for loop counters
						}
					}
				}
			}
		}
	}

	return vars
}

// extractForLoopVariablesFallback is the fallback implementation using string parsing
func extractForLoopVariablesFallback(exprValue string) map[string]*symbolTypeInfo {
	vars := make(map[string]*symbolTypeInfo)
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
						vars[varName] = &symbolTypeInfo{
							fullType: "int",
						}
					} else {
						vars[varName] = &symbolTypeInfo{
							fullType: "interface{}",
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
					vars[varName] = &symbolTypeInfo{
						fullType: "int",
					}
				}
			}
		}
	}

	return vars
}

// extractIfConditionVariables extracts variables from if condition using the AST
// e.g., "if err := doSomething(); err != nil" -> ["err"]
func extractIfConditionVariables(expr parser.Expression) map[string]*symbolTypeInfo {
	vars := make(map[string]*symbolTypeInfo)

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
						vars[ident.Name] = &symbolTypeInfo{
							fullType: "interface{}",
						}
					}
				}
			}
		}
	}

	return vars
}

// extractIfConditionVariablesFallback is the fallback implementation using string parsing
func extractIfConditionVariablesFallback(exprValue string) map[string]*symbolTypeInfo {
	vars := make(map[string]*symbolTypeInfo)
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
						vars[varName] = &symbolTypeInfo{
							fullType: "interface{}",
						}
					}
				}
			}
		}
	}

	return vars
}
