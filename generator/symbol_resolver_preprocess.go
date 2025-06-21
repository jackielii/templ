package generator

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/a-h/templ/parser/v2"
)

// PreprocessResult contains the results of preprocessing templ files
type PreprocessResult struct {
	// ElementComponentsDetected indicates if any ElementComponent syntax was found
	ElementComponentsDetected bool
	// TemplFiles contains all templ files found during preprocessing
	TemplFiles []string
	// DependencyGraph contains the package dependency structure
	DependencyGraph *dependencyGraph
}

// GetInternalPackages returns all packages that are internal (have templ files)
func (r *PreprocessResult) GetInternalPackages() []string {
	if r.DependencyGraph == nil {
		return nil
	}
	return r.DependencyGraph.getInternalPackages()
}

// PreprocessTemplFiles scans all templ files to detect ElementComponent usage
// and prepare the global symbol resolver
func PreprocessTemplFiles(rootDir string, templFiles []string) (*PreprocessResult, error) {
	// Debug: PreprocessTemplFiles called with %d files
	result := &PreprocessResult{
		TemplFiles:      templFiles,
		DependencyGraph: newDependencyGraph(),
	}

	// Phase 1: Scan all files and build initial package information
	packageHasElementComponent := make(map[string]bool)
	packageFiles := make(map[string][]string)
	packageImports := make(map[string]map[string]bool)

	for _, file := range templFiles {
		tf, err := parser.Parse(file)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", file, err)
		}

		// Determine package path from file location
		dir := filepath.Dir(file)
		pkgPath, err := globalSymbolResolver.getPackagePathFromDir(dir)
		if err != nil {
			// If we can't determine package path, use directory as fallback
			pkgPath = dir
		}

		// Check for ElementComponent syntax
		hasElementComponent, err := detectElementComponentSyntax(file)
		if err != nil {
			return nil, fmt.Errorf("failed to detect element syntax in %s: %w", file, err)
		}

		if hasElementComponent {
			packageHasElementComponent[pkgPath] = true
			result.ElementComponentsDetected = true
		}

		// Track files per package
		packageFiles[pkgPath] = append(packageFiles[pkgPath], file)

		// Extract imports
		if packageImports[pkgPath] == nil {
			packageImports[pkgPath] = make(map[string]bool)
		}

		for _, node := range tf.Nodes {
			if goExpr, ok := node.(*parser.TemplateFileGoExpression); ok {
				if imports := extractImports(goExpr.Expression.Value); len(imports) > 0 {
					for _, imp := range imports {
						packageImports[pkgPath][imp] = true
					}
				}
			}
		}
	}

	// If no ElementComponent syntax detected, return early
	if !result.ElementComponentsDetected {
		return result, nil
	}

	// Phase 2: Build dependency graph
	// Add all packages to the graph
	for pkg, files := range packageFiles {
		_ = result.DependencyGraph.addPackage(pkg, filepath.Dir(files[0]), packageHasElementComponent[pkg])
		for _, file := range files {
			result.DependencyGraph.addTemplFile(pkg, file)
		}
	}

	// Add dependencies
	for pkg, imports := range packageImports {
		for imp := range imports {
			result.DependencyGraph.addDependency(pkg, imp)
		}
	}

	// Phase 3: Identify internal packages (those in ElementComponent dependency trees)
	result.DependencyGraph.buildInternalPackages()

	// Phase 4: Get internal packages and sort them topologically
	internalPackages := result.DependencyGraph.getInternalPackages()
	sortedPackages, err := result.DependencyGraph.topologicalSort(internalPackages)
	if err != nil {
		return nil, fmt.Errorf("failed to sort internal packages: %w", err)
	}

	// Phase 5: Build overlays only for internal packages
	fmt.Printf("Debug: Building overlays for %d internal packages\n", len(internalPackages))
	for _, pkg := range internalPackages {
		node, exists := result.DependencyGraph.getPackageInfo(pkg)
		if !exists || len(node.templFiles) == 0 {
			continue
		}

		fmt.Printf("Debug: Processing package %s with %d templ files\n", pkg, len(node.templFiles))
		for _, file := range node.templFiles {
			tf, err := parser.Parse(file)
			if err != nil {
				continue // Already parsed successfully above
			}
			fmt.Printf("Debug: Registering overlay for %s\n", file)
			if err := globalSymbolResolver.registerTemplOverlay(tf, file); err != nil {
				return nil, fmt.Errorf("failed to register overlay for %s: %w", file, err)
			}
		}
	}

	// Phase 6: Preload all internal packages in topological order
	for _, pkg := range sortedPackages {
		node, exists := result.DependencyGraph.getPackageInfo(pkg)
		if !exists || node.directory == "" {
			continue
		}

		// Force package loading with overlays to populate type information
		if _, err := globalSymbolResolver.ensurePackageLoaded(node.directory, ""); err != nil {
			// Don't fail on package load errors, just log them
			fmt.Printf("Warning: failed to load package %s: %v\n", pkg, err)
		}
	}

	// Phase 7: Parse local templates into the resolver cache for internal packages
	for _, pkg := range internalPackages {
		node, exists := result.DependencyGraph.getPackageInfo(pkg)
		if !exists {
			continue
		}

		for _, file := range node.templFiles {
			tf, err := parser.Parse(file)
			if err != nil {
				continue
			}

			// Extract local templates and cache them
			for _, n := range tf.Nodes {
				if tmpl, ok := n.(*parser.HTMLTemplate); ok {
					sig := extractTemplateSignature(tmpl, pkg)
					if sig.name != "" {
						// Debug
						if sig.name == "Button" {
							fmt.Printf("Debug PreprocessTemplFiles: Caching Button with %d params\n", len(sig.parameters))
							for _, p := range sig.parameters {
								fmt.Printf("  - %s: %s\n", p.name, p.typ)
							}
						}
						// Only cache by qualified name to avoid conflicts
						globalSymbolResolver.signatures[sig.qualifiedName] = sig
					}
				}
			}
		}
	}

	// Store the dependency graph
	globalSymbolResolver.depGraph = result.DependencyGraph

	return result, nil
}

// extractTemplateSignature extracts signature from an HTMLTemplate node
func extractTemplateSignature(tmpl *parser.HTMLTemplate, pkgPath string) componentSignature {
	// Extract template name from the expression value
	// The expression value is like "Container(child templ.Component)"
	exprValue := strings.TrimSpace(tmpl.Expression.Value)

	var name string
	// Check if this is a struct method template
	if strings.HasPrefix(exprValue, "(") {
		// Find the closing parenthesis for the receiver
		if idx := strings.Index(exprValue, ")"); idx != -1 {
			// Extract the method name after the receiver
			methodPart := strings.TrimSpace(exprValue[idx+1:])
			if methodIdx := strings.Index(methodPart, "("); methodIdx != -1 {
				name = strings.TrimSpace(methodPart[:methodIdx])
			} else {
				name = methodPart
			}
		}
	} else {
		// Regular template: extract name before the first parenthesis
		if idx := strings.Index(exprValue, "("); idx != -1 {
			name = strings.TrimSpace(exprValue[:idx])
		} else {
			name = exprValue
		}
	}
	if name == "" {
		return componentSignature{}
	}

	// Parse parameters from the template expression
	var params []parameterInfo

	// Simple parameter extraction - this is a basic implementation
	// Real implementation should use proper Go AST parsing
	if openParen := strings.Index(exprValue, "("); openParen != -1 {
		if closeParen := strings.LastIndex(exprValue, ")"); closeParen > openParen {
			paramStr := exprValue[openParen+1 : closeParen]
			if paramStr != "" {
				// Very basic parsing - split by comma
				paramParts := strings.Split(paramStr, ",")
				for _, part := range paramParts {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					// Extract name and type
					tokens := strings.Fields(part)
					if len(tokens) >= 2 {
						paramName := tokens[0]
						paramType := strings.Join(tokens[1:], " ")
						param := parameterInfo{
							name: paramName,
							typ:  paramType,
						}
						// Debug
						if name == "Button" {
							fmt.Printf("Debug extractTemplateSignature: Button param '%s' -> name='%s', type='%s'\n", part, paramName, paramType)
						}
						// Check for special types
						if strings.Contains(paramType, "templ.Component") {
							param.isComponent = true
						}
						if strings.Contains(paramType, "templ.Attributer") {
							param.isAttributer = true
						}
						params = append(params, param)
					}
				}
			}
		}
	}

	return componentSignature{
		packagePath:   pkgPath,
		name:          name,
		qualifiedName: pkgPath + "." + name,
		parameters:    params,
		isStruct:      false,
	}
}

// detectElementComponentSyntax quickly scans a file for ElementComponent syntax
func detectElementComponentSyntax(fileName string) (bool, error) {
	tf, err := parser.Parse(fileName)
	if err != nil {
		return false, err
	}

	var detected bool
	var walkChildren func(children []parser.Node)
	walkChildren = func(children []parser.Node) {
		if detected {
			return
		}
		for _, child := range children {
			switch n := child.(type) {
			case *parser.Element:
				// Check if this is an ElementComponent (uppercase first letter)
				if n.Name != "" && len(n.Name) > 0 && n.Name[0] >= 'A' && n.Name[0] <= 'Z' {
					// Debug: Found ElementComponent in test-element-component
					detected = true
					return
				}
				walkChildren(n.Children)
			case *parser.ElementComponent:
				// This is an ElementComponent like <Button />
				// Debug: Found ElementComponent in test-element-component
				detected = true
				return
			case *parser.TemplElementExpression:
				// This is likely an ElementComponent call like <Button />
				// Check if the expression starts with uppercase
				exprStr := strings.TrimSpace(n.Expression.Value)
				if len(exprStr) > 0 && exprStr[0] >= 'A' && exprStr[0] <= 'Z' {
					detected = true
					return
				}
				walkChildren(n.Children)
			case *parser.IfExpression:
				walkChildren(n.Then)
				walkChildren(n.Else)
			case *parser.SwitchExpression:
				for _, c := range n.Cases {
					walkChildren(c.Children)
				}
			case *parser.ForExpression:
				walkChildren(n.Children)
			}
		}
	}

	// Walk through template nodes
	for _, node := range tf.Nodes {
		if detected {
			break
		}
		if tmpl, ok := node.(*parser.HTMLTemplate); ok {
			walkChildren(tmpl.Children)
		}
	}

	return detected, nil
}

func extractImports2() {}

// extractImports extracts import paths from Go code
func extractImports(goCode string) []string {
	var imports []string

	// Simple heuristic - look for import statements
	lines := strings.Split(goCode, "\n")
	inImportBlock := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for import block start
		if strings.HasPrefix(line, "import (") {
			inImportBlock = true
			continue
		}

		// Check for import block end
		if inImportBlock && line == ")" {
			inImportBlock = false
			continue
		}

		// Single line import
		if strings.HasPrefix(line, "import ") && strings.Contains(line, "\"") {
			if path := extractImportPath(line); path != "" {
				imports = append(imports, path)
			}
		}

		// Import within block
		if inImportBlock && strings.Contains(line, "\"") {
			if path := extractImportPath(line); path != "" {
				imports = append(imports, path)
			}
		}
	}

	return imports
}

// extractImportPath extracts the import path from a line
func extractImportPath(line string) string {
	// Find quoted string
	start := strings.Index(line, "\"")
	if start == -1 {
		return ""
	}
	end := strings.Index(line[start+1:], "\"")
	if end == -1 {
		return ""
	}
	return line[start+1 : start+1+end]
}
