package generator

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// dependencyGraph represents the package dependency structure for ElementComponent resolution
type dependencyGraph struct {
	// nodes maps package path to node information
	nodes map[string]*packageNode
	
	// roots contains packages that use ElementComponent syntax
	roots map[string]bool
	
	// internalPackages contains all packages in the dependency trees
	// (roots and their transitive dependencies)
	internalPackages map[string]bool
}

// packageNode represents a package in the dependency graph
type packageNode struct {
	// packagePath is the import path of the package
	packagePath string
	
	// directory is the filesystem path to the package
	directory string
	
	// templFiles are the .templ files in this package
	templFiles []string
	
	// hasElementComponent indicates if this package uses ElementComponent syntax
	hasElementComponent bool
	
	// dependencies are the packages this package imports
	dependencies []string
	
	// dependents are the packages that import this package
	dependents []string
}

// newDependencyGraph creates a new dependency graph
func newDependencyGraph() *dependencyGraph {
	return &dependencyGraph{
		nodes:            make(map[string]*packageNode),
		roots:            make(map[string]bool),
		internalPackages: make(map[string]bool),
	}
}

// addPackage adds a package to the graph
func (g *dependencyGraph) addPackage(pkgPath, directory string, hasElementComponent bool) *packageNode {
	if node, exists := g.nodes[pkgPath]; exists {
		// Update existing node
		if hasElementComponent {
			node.hasElementComponent = true
			g.roots[pkgPath] = true
		}
		return node
	}
	
	node := &packageNode{
		packagePath:         pkgPath,
		directory:           directory,
		hasElementComponent: hasElementComponent,
		dependencies:        []string{},
		dependents:          []string{},
		templFiles:          []string{},
	}
	
	g.nodes[pkgPath] = node
	
	if hasElementComponent {
		g.roots[pkgPath] = true
	}
	
	return node
}

// addDependency adds a dependency relationship between packages
func (g *dependencyGraph) addDependency(fromPkg, toPkg string) {
	// Ensure both packages exist
	fromNode := g.nodes[fromPkg]
	if fromNode == nil {
		fromNode = g.addPackage(fromPkg, "", false)
	}
	
	toNode := g.nodes[toPkg]
	if toNode == nil {
		toNode = g.addPackage(toPkg, "", false)
	}
	
	// Add dependency relationship
	fromNode.dependencies = appendUnique(fromNode.dependencies, toPkg)
	toNode.dependents = appendUnique(toNode.dependents, fromPkg)
}

// addTemplFile adds a templ file to a package
func (g *dependencyGraph) addTemplFile(pkgPath, templFile string) {
	node := g.nodes[pkgPath]
	if node == nil {
		// Determine directory from file path
		dir := filepath.Dir(templFile)
		node = g.addPackage(pkgPath, dir, false)
	}
	node.templFiles = appendUnique(node.templFiles, templFile)
}

// buildInternalPackages identifies all packages that are part of ElementComponent dependency trees
func (g *dependencyGraph) buildInternalPackages() {
	// Clear existing internal packages
	g.internalPackages = make(map[string]bool)
	
	// Only packages with templ files should be considered internal
	// External packages (stdlib, third-party) won't have templ files
	for pkgPath, node := range g.nodes {
		if len(node.templFiles) > 0 {
			g.internalPackages[pkgPath] = true
		}
	}
}

// isInternal returns true if the package is part of an ElementComponent dependency tree
func (g *dependencyGraph) isInternal(pkgPath string) bool {
	return g.internalPackages[pkgPath]
}

// getRoots returns all packages that use ElementComponent syntax
func (g *dependencyGraph) getRoots() []string {
	roots := make([]string, 0, len(g.roots))
	for root := range g.roots {
		roots = append(roots, root)
	}
	return roots
}

// getInternalPackages returns all packages in ElementComponent dependency trees
func (g *dependencyGraph) getInternalPackages() []string {
	packages := make([]string, 0, len(g.internalPackages))
	for pkg := range g.internalPackages {
		packages = append(packages, pkg)
	}
	return packages
}

// topologicalSort returns packages in topological order (dependencies before dependents)
func (g *dependencyGraph) topologicalSort(packages []string) ([]string, error) {
	// Build in-degree map for the specified packages
	inDegree := make(map[string]int)
	relevantNodes := make(map[string]*packageNode)
	
	for _, pkg := range packages {
		node := g.nodes[pkg]
		if node == nil {
			continue
		}
		relevantNodes[pkg] = node
		inDegree[pkg] = 0
	}
	
	// Calculate in-degrees (only count dependencies within the package set)
	for _, pkg := range packages {
		node := g.nodes[pkg]
		if node == nil {
			continue
		}
		for _, dep := range node.dependencies {
			if _, relevant := relevantNodes[dep]; relevant {
				inDegree[pkg]++
			}
		}
	}
	
	// Find all nodes with in-degree 0 (no dependencies in the set)
	var queue []string
	for pkg, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, pkg)
		}
	}
	
	var sorted []string
	for len(queue) > 0 {
		// Pop from queue
		pkg := queue[0]
		queue = queue[1:]
		sorted = append(sorted, pkg)
		
		// Reduce in-degree for dependents
		node := g.nodes[pkg]
		if node != nil {
			for _, dependent := range node.dependents {
				if _, relevant := relevantNodes[dependent]; !relevant {
					continue
				}
				inDegree[dependent]--
				if inDegree[dependent] == 0 {
					queue = append(queue, dependent)
				}
			}
		}
	}
	
	// Check for cycles
	if len(sorted) != len(relevantNodes) {
		return nil, fmt.Errorf("dependency cycle detected among packages")
	}
	
	return sorted, nil
}

// getPackageInfo returns information about a package
func (g *dependencyGraph) getPackageInfo(pkgPath string) (*packageNode, bool) {
	node, exists := g.nodes[pkgPath]
	return node, exists
}

// appendUnique appends a string to a slice if it's not already present
func appendUnique(slice []string, item string) []string {
	if slices.Contains(slice, item) {
		return slice
	}
	return append(slice, item)
}

// filterLocalPackages filters out external packages (stdlib, third-party) from a package list
func filterLocalPackages(packages []string, moduleRoot string) []string {
	var local []string
	for _, pkg := range packages {
		// Skip standard library packages (no dots in package path)
		if !strings.Contains(pkg, ".") {
			continue
		}
		
		// Skip common third-party packages (this is a simple heuristic)
		// In a real implementation, we'd check against go.mod
		if strings.HasPrefix(pkg, "github.com/") || 
		   strings.HasPrefix(pkg, "golang.org/") ||
		   strings.HasPrefix(pkg, "google.golang.org/") {
			// Only include if it's our module
			if moduleRoot != "" && strings.HasPrefix(pkg, moduleRoot) {
				local = append(local, pkg)
			}
			continue
		}
		
		local = append(local, pkg)
	}
	return local
}