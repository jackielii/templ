package generator

import (
	"fmt"
	"sort"
	"testing"
)

func TestDependencyGraph_AddPackage(t *testing.T) {
	tests := []struct {
		name                string
		packages            []struct{ path, dir string; hasEC bool }
		expectedRoots       []string
		expectedNodeCount   int
	}{
		{
			name: "single package without ElementComponent",
			packages: []struct{ path, dir string; hasEC bool }{
				{"example.com/pkg1", "/path/to/pkg1", false},
			},
			expectedRoots:     []string{},
			expectedNodeCount: 1,
		},
		{
			name: "single package with ElementComponent",
			packages: []struct{ path, dir string; hasEC bool }{
				{"example.com/pkg1", "/path/to/pkg1", true},
			},
			expectedRoots:     []string{"example.com/pkg1"},
			expectedNodeCount: 1,
		},
		{
			name: "multiple packages mixed",
			packages: []struct{ path, dir string; hasEC bool }{
				{"example.com/pkg1", "/path/to/pkg1", true},
				{"example.com/pkg2", "/path/to/pkg2", false},
				{"example.com/pkg3", "/path/to/pkg3", true},
			},
			expectedRoots:     []string{"example.com/pkg1", "example.com/pkg3"},
			expectedNodeCount: 3,
		},
		{
			name: "updating existing package to have ElementComponent",
			packages: []struct{ path, dir string; hasEC bool }{
				{"example.com/pkg1", "/path/to/pkg1", false},
				{"example.com/pkg1", "/path/to/pkg1", true}, // Update
			},
			expectedRoots:     []string{"example.com/pkg1"},
			expectedNodeCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newDependencyGraph()

			for _, pkg := range tt.packages {
				g.addPackage(pkg.path, pkg.dir, pkg.hasEC)
			}

			// Check node count
			if len(g.nodes) != tt.expectedNodeCount {
				t.Errorf("expected %d nodes, got %d", tt.expectedNodeCount, len(g.nodes))
			}

			// Check roots
			roots := g.getRoots()
			sort.Strings(roots)
			sort.Strings(tt.expectedRoots)
			
			if len(roots) != len(tt.expectedRoots) {
				t.Errorf("expected %d roots, got %d", len(tt.expectedRoots), len(roots))
			}
			
			for i, root := range roots {
				if i >= len(tt.expectedRoots) || root != tt.expectedRoots[i] {
					t.Errorf("root mismatch at index %d: expected %q, got %q", i, tt.expectedRoots[i], root)
				}
			}
		})
	}
}

func TestDependencyGraph_AddDependency(t *testing.T) {
	g := newDependencyGraph()

	// Add dependency between non-existent packages
	g.addDependency("pkg1", "pkg2")

	// Verify both packages were created
	if _, exists := g.nodes["pkg1"]; !exists {
		t.Error("pkg1 should have been created")
	}
	if _, exists := g.nodes["pkg2"]; !exists {
		t.Error("pkg2 should have been created")
	}

	// Verify dependency relationship
	pkg1 := g.nodes["pkg1"]
	if len(pkg1.dependencies) != 1 || pkg1.dependencies[0] != "pkg2" {
		t.Error("pkg1 should depend on pkg2")
	}

	pkg2 := g.nodes["pkg2"]
	if len(pkg2.dependents) != 1 || pkg2.dependents[0] != "pkg1" {
		t.Error("pkg2 should have pkg1 as dependent")
	}

	// Add duplicate dependency
	g.addDependency("pkg1", "pkg2")
	if len(pkg1.dependencies) != 1 {
		t.Error("duplicate dependency should not be added")
	}
}

func TestDependencyGraph_AddTemplFile(t *testing.T) {
	g := newDependencyGraph()

	// Add templ file to non-existent package
	g.addTemplFile("example.com/pkg1", "/path/to/pkg1/file.templ")

	node, exists := g.nodes["example.com/pkg1"]
	if !exists {
		t.Fatal("package should have been created")
	}

	if node.directory != "/path/to/pkg1" {
		t.Errorf("expected directory %q, got %q", "/path/to/pkg1", node.directory)
	}

	if len(node.templFiles) != 1 || node.templFiles[0] != "/path/to/pkg1/file.templ" {
		t.Error("templ file not added correctly")
	}

	// Add another file
	g.addTemplFile("example.com/pkg1", "/path/to/pkg1/file2.templ")
	if len(node.templFiles) != 2 {
		t.Error("second templ file not added")
	}

	// Add duplicate file
	g.addTemplFile("example.com/pkg1", "/path/to/pkg1/file.templ")
	if len(node.templFiles) != 2 {
		t.Error("duplicate file should not be added")
	}
}

func TestDependencyGraph_BuildInternalPackages(t *testing.T) {
	tests := []struct {
		name             string
		setup            func(*dependencyGraph)
		expectedInternal []string
	}{
		{
			name: "only packages with templ files are internal",
			setup: func(g *dependencyGraph) {
				// Package with templ files
				g.addPackage("example.com/internal1", "/path/internal1", true)
				g.addTemplFile("example.com/internal1", "/path/internal1/file.templ")
				
				// Package without templ files (external dependency)
				g.addPackage("github.com/external/lib", "", false)
				
				// Standard library package
				g.addPackage("fmt", "", false)
				
				// Another internal package with templ files
				g.addPackage("example.com/internal2", "/path/internal2", false)
				g.addTemplFile("example.com/internal2", "/path/internal2/file.templ")
				
				// Set up dependencies
				g.addDependency("example.com/internal1", "github.com/external/lib")
				g.addDependency("example.com/internal1", "fmt")
				g.addDependency("example.com/internal1", "example.com/internal2")
			},
			expectedInternal: []string{"example.com/internal1", "example.com/internal2"},
		},
		{
			name: "ElementComponent roots with external dependencies",
			setup: func(g *dependencyGraph) {
				// Root with ElementComponent
				g.addPackage("example.com/ui", "/path/ui", true)
				g.addTemplFile("example.com/ui", "/path/ui/button.templ")
				
				// Internal dependency with templ files
				g.addPackage("example.com/components", "/path/components", false)
				g.addTemplFile("example.com/components", "/path/components/base.templ")
				
				// External dependencies without templ files
				g.addPackage("github.com/gorilla/mux", "", false)
				g.addPackage("database/sql", "", false)
				
				g.addDependency("example.com/ui", "example.com/components")
				g.addDependency("example.com/ui", "github.com/gorilla/mux")
				g.addDependency("example.com/ui", "database/sql")
			},
			expectedInternal: []string{"example.com/ui", "example.com/components"},
		},
		{
			name: "no packages with templ files",
			setup: func(g *dependencyGraph) {
				g.addPackage("example.com/pkg1", "/path/pkg1", false)
				g.addPackage("example.com/pkg2", "/path/pkg2", false)
				g.addDependency("example.com/pkg1", "example.com/pkg2")
			},
			expectedInternal: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newDependencyGraph()
			tt.setup(g)
			
			g.buildInternalPackages()
			
			internal := g.getInternalPackages()
			sort.Strings(internal)
			sort.Strings(tt.expectedInternal)
			
			if len(internal) != len(tt.expectedInternal) {
				t.Errorf("expected %d internal packages, got %d", len(tt.expectedInternal), len(internal))
				t.Errorf("expected: %v", tt.expectedInternal)
				t.Errorf("got: %v", internal)
				return
			}
			
			for i, pkg := range internal {
				if pkg != tt.expectedInternal[i] {
					t.Errorf("internal package mismatch at index %d: expected %q, got %q", 
						i, tt.expectedInternal[i], pkg)
				}
			}
		})
	}
}

func TestDependencyGraph_TopologicalSort(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*dependencyGraph) []string // Returns packages to sort
		expectError bool
		validate    func([]string) error // Custom validation function
	}{
		{
			name: "simple linear dependency",
			setup: func(g *dependencyGraph) []string {
				g.addPackage("a", "", false)
				g.addPackage("b", "", false)
				g.addPackage("c", "", false)
				g.addDependency("a", "b")
				g.addDependency("b", "c")
				return []string{"a", "b", "c"}
			},
			validate: func(sorted []string) error {
				// c should come before b, b before a
				cIdx, bIdx, aIdx := -1, -1, -1
				for i, pkg := range sorted {
					switch pkg {
					case "a":
						aIdx = i
					case "b":
						bIdx = i
					case "c":
						cIdx = i
					}
				}
				if cIdx >= bIdx || bIdx >= aIdx {
					return errorf("invalid order: c=%d, b=%d, a=%d", cIdx, bIdx, aIdx)
				}
				return nil
			},
		},
		{
			name: "diamond dependency",
			setup: func(g *dependencyGraph) []string {
				g.addPackage("a", "", false)
				g.addPackage("b", "", false)
				g.addPackage("c", "", false)
				g.addPackage("d", "", false)
				g.addDependency("a", "b")
				g.addDependency("a", "c")
				g.addDependency("b", "d")
				g.addDependency("c", "d")
				return []string{"a", "b", "c", "d"}
			},
			validate: func(sorted []string) error {
				// d before b and c, b and c before a
				positions := make(map[string]int)
				for i, pkg := range sorted {
					positions[pkg] = i
				}
				if positions["d"] >= positions["b"] || positions["d"] >= positions["c"] {
					return errorf("d should come before b and c")
				}
				if positions["b"] >= positions["a"] || positions["c"] >= positions["a"] {
					return errorf("b and c should come before a")
				}
				return nil
			},
		},
		{
			name: "circular dependency",
			setup: func(g *dependencyGraph) []string {
				g.addPackage("a", "", false)
				g.addPackage("b", "", false)
				g.addPackage("c", "", false)
				g.addDependency("a", "b")
				g.addDependency("b", "c")
				g.addDependency("c", "a")
				return []string{"a", "b", "c"}
			},
			expectError: true,
		},
		{
			name: "independent packages",
			setup: func(g *dependencyGraph) []string {
				g.addPackage("a", "", false)
				g.addPackage("b", "", false)
				g.addPackage("c", "", false)
				return []string{"a", "b", "c"}
			},
			validate: func(sorted []string) error {
				// All packages should be present, order doesn't matter
				if len(sorted) != 3 {
					return errorf("expected 3 packages, got %d", len(sorted))
				}
				return nil
			},
		},
		{
			name: "partial package list",
			setup: func(g *dependencyGraph) []string {
				g.addPackage("a", "", false)
				g.addPackage("b", "", false)
				g.addPackage("c", "", false)
				g.addPackage("d", "", false)
				g.addDependency("a", "b")
				g.addDependency("b", "c")
				g.addDependency("c", "d")
				// Only sort a subset
				return []string{"a", "b", "c"}
			},
			validate: func(sorted []string) error {
				// c before b, b before a (d is ignored)
				positions := make(map[string]int)
				for i, pkg := range sorted {
					positions[pkg] = i
				}
				if positions["c"] >= positions["b"] || positions["b"] >= positions["a"] {
					return errorf("invalid order within subset")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newDependencyGraph()
			packages := tt.setup(g)
			
			sorted, err := g.topologicalSort(packages)
			
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			
			if tt.validate != nil {
				if err := tt.validate(sorted); err != nil {
					t.Errorf("validation failed: %v", err)
					t.Errorf("sorted order: %v", sorted)
				}
			}
		})
	}
}

func TestDependencyGraph_IsInternal(t *testing.T) {
	g := newDependencyGraph()
	
	// Set up packages
	g.addPackage("internal/pkg1", "/path/internal1", true)
	g.addTemplFile("internal/pkg1", "/path/internal1/file.templ")
	
	g.addPackage("external/pkg2", "/path/external", false)
	// No templ files for external package
	
	g.buildInternalPackages()
	
	if !g.isInternal("internal/pkg1") {
		t.Error("internal/pkg1 should be internal")
	}
	
	if g.isInternal("external/pkg2") {
		t.Error("external/pkg2 should not be internal")
	}
	
	if g.isInternal("non/existent") {
		t.Error("non-existent package should not be internal")
	}
}

func TestFilterLocalPackages(t *testing.T) {
	tests := []struct {
		name       string
		packages   []string
		moduleRoot string
		expected   []string
	}{
		{
			name: "filter standard library",
			packages: []string{
				"fmt",
				"strings",
				"example.com/myapp",
			},
			moduleRoot: "example.com/myapp",
			expected:   []string{"example.com/myapp"},
		},
		{
			name: "filter third-party packages",
			packages: []string{
				"github.com/gorilla/mux",
				"golang.org/x/tools",
				"google.golang.org/grpc",
				"example.com/myapp",
			},
			moduleRoot: "example.com/myapp",
			expected:   []string{"example.com/myapp"},
		},
		{
			name: "keep module's packages from github",
			packages: []string{
				"github.com/myorg/myapp/pkg1",
				"github.com/myorg/myapp/pkg2",
				"github.com/other/lib",
			},
			moduleRoot: "github.com/myorg/myapp",
			expected: []string{
				"github.com/myorg/myapp/pkg1",
				"github.com/myorg/myapp/pkg2",
			},
		},
		{
			name: "empty module root keeps only non-vendor packages",
			packages: []string{
				"myapp/internal",
				"github.com/external/lib",
			},
			moduleRoot: "",
			expected:   []string{"myapp/internal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterLocalPackages(tt.packages, tt.moduleRoot)
			
			sort.Strings(result)
			sort.Strings(tt.expected)
			
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d packages, got %d", len(tt.expected), len(result))
				t.Errorf("expected: %v", tt.expected)
				t.Errorf("got: %v", result)
				return
			}
			
			for i, pkg := range result {
				if pkg != tt.expected[i] {
					t.Errorf("package mismatch at index %d: expected %q, got %q", 
						i, tt.expected[i], pkg)
				}
			}
		})
	}
}

// Helper function to create formatted errors
func errorf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}