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

	"github.com/a-h/templ/cmd/templ/generatecmd/modcheck"
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

// ParameterInfo represents a function parameter or struct field
type ParameterInfo struct {
	Name string
	Type string
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


// SymbolResolver automatically detects module roots and provides unified resolution
// for both templ templates and Go components across packages
type SymbolResolver struct {
	signatures map[string]ComponentSignature // Unified cache for all component signatures
	overlay    map[string][]byte             // Go file overlays for templ templates
}

// newSymbolResolver creates a new symbol resolver
func newSymbolResolver() SymbolResolver {
	return SymbolResolver{
		signatures: make(map[string]ComponentSignature),
		overlay:    make(map[string][]byte),
	}
}

// ResolveComponentFrom resolves a component starting from a specific directory
// This is the primary method used during code generation
func (r *SymbolResolver) ResolveComponentFrom(fromDir, pkgPath, componentName string) (ComponentSignature, error) {
	return r.ResolveComponentWithPosition(fromDir, pkgPath, componentName, parser.Position{}, "")
}

// ResolveComponentWithPosition resolves a component with position information for error reporting
func (r *SymbolResolver) ResolveComponentWithPosition(fromDir, pkgPath, componentName string, pos parser.Position, fileName string) (ComponentSignature, error) {
	// Generate cache key based on context
	var cacheKey string
	if pkgPath == "" {
		cacheKey = "local:" + componentName
	} else {
		cacheKey = "pkg:" + pkgPath + ":" + componentName
	}
	
	// Check unified cache first
	if sig, ok := r.signatures[cacheKey]; ok {
		return sig, nil
	}

	// Find the module root for the directory we're starting from
	moduleRoot, err := modcheck.WalkUp(fromDir)
	if err != nil {
		baseErr := fmt.Errorf("failed to find module root from %s: %w", fromDir, err)
		if fileName != "" {
			return ComponentSignature{}, ComponentResolutionError{Err: baseErr, Position: pos, FileName: fileName}
		}
		return ComponentSignature{}, baseErr
	}

	// Load the package with full type information
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedTypesInfo |
			packages.NeedModule,
		Dir: moduleRoot, // Use the detected module root
	}

	pkgs, err := packages.Load(cfg, pkgPath)
	if err != nil {
		baseErr := fmt.Errorf("failed to load package %s: %w", pkgPath, err)
		if fileName != "" {
			return ComponentSignature{}, ComponentResolutionError{Err: baseErr, Position: pos, FileName: fileName}
		}
		return ComponentSignature{}, baseErr
	}

	if len(pkgs) == 0 {
		baseErr := fmt.Errorf("package %s not found", pkgPath)
		if fileName != "" {
			return ComponentSignature{}, ComponentResolutionError{Err: baseErr, Position: pos, FileName: fileName}
		}
		return ComponentSignature{}, baseErr
	}

	pkg := pkgs[0]
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

	// Cache the signature in unified cache
	r.signatures[cacheKey] = sig
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
func (r *SymbolResolver) processTemplFiles(pkgDir, pkgName string) error {
	templFiles, err := filepath.Glob(filepath.Join(pkgDir, "*.templ"))
	if err != nil {
		return fmt.Errorf("failed to find templ files in %s: %w", pkgDir, err)
	}

	for _, templFile := range templFiles {
		content, err := os.ReadFile(templFile)
		if err != nil {
			// Skip files we can't read, but log the error
			continue
		}

		tf, err := parser.ParseString(string(content))
		if err != nil {
			// Skip files that fail to parse, but log the error
			continue
		}

		// Generate overlay path and content
		overlayPath := strings.TrimSuffix(templFile, ".templ") + "_templ_overlay.go"
		overlayContent := r.generateOverlay(tf, pkgName)
		r.overlay[overlayPath] = []byte(overlayContent)
	}

	return nil
}

// generateOverlay creates Go stub code for templ templates
func (r *SymbolResolver) generateOverlay(tf *parser.TemplateFile, pkgName string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("package %s\n\n", pkgName))
	sb.WriteString("import (\n")
	sb.WriteString("\t\"github.com/a-h/templ\"\n")
	sb.WriteString("\t\"context\"\n")
	sb.WriteString("\t\"io\"\n")
	sb.WriteString(")\n\n")

	// Generate function stubs for each templ template
	for _, node := range tf.Nodes {
		if tmpl, ok := node.(*parser.HTMLTemplate); ok {
			sb.WriteString("func ")
			sb.WriteString(tmpl.Expression.Value)
			sb.WriteString(" templ.Component {\n")
			sb.WriteString("\treturn nil\n")
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
			paramInfo = append(paramInfo, ParameterInfo{
				Name: param.Name(),
				Type: param.Type().String(),
			})
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
		paramInfo = r.extractStructFields(typeName.Type())
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
		paramInfo = append(paramInfo, ParameterInfo{
			Name: param.Name(),
			Type: param.Type().String(),
		})
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
// This replaces the functionality of TemplSignatureResolver.ExtractSignatures
func (r *SymbolResolver) ExtractSignatures(tf *parser.TemplateFile) {
	// Clear previous local templates from cache
	// Find and remove all keys starting with "local:"
	for key := range r.signatures {
		if strings.HasPrefix(key, "local:") {
			delete(r.signatures, key)
		}
	}
	
	for _, node := range tf.Nodes {
		switch n := node.(type) {
		case *parser.HTMLTemplate:
			sig, ok := r.extractHTMLTemplateSignature(n)
			if ok {
				r.addLocalTemplate(sig)
			}
		case *parser.TemplateFileGoExpression:
			// Extract type definitions that might implement Component
			r.extractGoTypeSignatures(n)
		}
	}
}

// GetLocalTemplate returns a local template signature by name
func (r *SymbolResolver) GetLocalTemplate(name string) (ComponentSignature, bool) {
	sig, ok := r.signatures["local:"+name]
	return sig, ok
}

// AddLocalTemplateAlias adds an alias for a local template
func (r *SymbolResolver) AddLocalTemplateAlias(alias, target string) {
	if sig, ok := r.signatures["local:"+target]; ok {
		r.signatures["local:"+alias] = sig
	}
}

// GetAllLocalTemplateNames returns all local template names for debugging
func (r *SymbolResolver) GetAllLocalTemplateNames() []string {
	var names []string
	for key := range r.signatures {
		if strings.HasPrefix(key, "local:") {
			names = append(names, strings.TrimPrefix(key, "local:"))
		}
	}
	return names
}

// AddComponentSignature adds a resolved component signature for code generation
func (r *SymbolResolver) AddComponentSignature(sig ComponentSignature) {
	r.signatures["qualified:"+sig.QualifiedName] = sig
}

// GetComponentSignature returns a component signature by qualified name
func (r *SymbolResolver) GetComponentSignature(qualifiedName string) (ComponentSignature, bool) {
	sig, ok := r.signatures["qualified:"+qualifiedName]
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

// addLocalTemplate adds a local template signature to unified cache
func (r *SymbolResolver) addLocalTemplate(sig ComponentSignature) {
	r.signatures["local:"+sig.Name] = sig
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

// extractParametersFromAST extracts parameter information from AST field list
func (r *SymbolResolver) extractParametersFromAST(fieldList *ast.FieldList) []ParameterInfo {
	if fieldList == nil || len(fieldList.List) == 0 {
		return nil
	}

	var params []ParameterInfo

	for _, field := range fieldList.List {
		fieldType := r.astTypeToString(field.Type)

		// Handle multiple names with the same type (e.g., "a, b string")
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				params = append(params, ParameterInfo{
					Name: name.Name,
					Type: fieldType,
				})
			}
		} else {
			// Handle anonymous parameters
			params = append(params, ParameterInfo{
				Name: "",
				Type: fieldType,
			})
		}
	}

	return params
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
func (r *SymbolResolver) extractGoTypeSignatures(goExpr *parser.TemplateFileGoExpression) {
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
							PackagePath:   "",
							Name:          receiverType,
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

// extractStructFields extracts exported struct fields for struct components
func (r *SymbolResolver) extractStructFields(t types.Type) []ParameterInfo {
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
			fields = append(fields, ParameterInfo{
				Name: field.Name(),
				Type: field.Type().String(),
			})
		}
	}

	return fields
}