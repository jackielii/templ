package generator

import (
	"path/filepath"
	"testing"
)

func TestDebugSymbolResolution(t *testing.T) {
	// Initialize the global symbol resolver
	r := newSymbolResolver()

	// Create test overlay content
	overlayContent := `package testelementcomponent

import (
	"github.com/a-h/templ"
)

var structComp StructComponent

type StructComponent struct {
	Name    string
	Child   templ.Component
	enabled bool
}

func (s StructComponent) Page(title string, attrs templ.Attributer) templ.Component {
	return templ.NopComponent
}
`

	// Register overlay
	overlayPath := "/Users/jackieli/personal/templ/generator/test-element-component/template_templ.go"
	r.overlay[overlayPath] = []byte(overlayContent)

	// Set up dependency graph (simulate internal package)
	r.depGraph = newDependencyGraph()
	r.depGraph.addPackage("github.com/a-h/templ/generator/test-element-component", "/Users/jackieli/personal/templ/generator/test-element-component", true)
	r.depGraph.addTemplFile("github.com/a-h/templ/generator/test-element-component", "/Users/jackieli/personal/templ/generator/test-element-component/template.templ")
	r.depGraph.buildInternalPackages()

	// Try to load the package using ensurePackageLoaded
	fromDir := "/Users/jackieli/personal/templ/generator/test-element-component"
	pkg, err := r.ensurePackageLoaded(fromDir, "")
	if err != nil {
		t.Fatalf("Failed to load package: %v", err)
	}

	t.Logf("Package loaded: %s", pkg.PkgPath)

	// Check scope
	if pkg.Types == nil {
		t.Fatal("Package Types is nil")
	}

	if pkg.Types.Scope() == nil {
		t.Fatal("Package Types.Scope() is nil")
	}

	// Look for structComp
	obj := pkg.Types.Scope().Lookup("structComp")
	if obj == nil {
		t.Error("Variable 'structComp' not found in package scope")
		names := pkg.Types.Scope().Names()
		t.Logf("Names in scope: %v", names)
	} else {
		t.Logf("Found structComp: %v", obj)
	}
}

func TestResolveStructMethod(t *testing.T) {
	// Initialize the global symbol resolver
	r := newSymbolResolver()

	// Create test overlay content
	overlayContent := `package testelementcomponent

import (
	"github.com/a-h/templ"
)

var structComp StructComponent

type StructComponent struct {
	Name    string
	Child   templ.Component
	enabled bool
}

func (s StructComponent) Page(title string, attrs templ.Attributer) templ.Component {
	return templ.NopComponent
}
`

	// Register overlay
	dir := "/Users/jackieli/personal/templ/generator/test-element-component"
	overlayPath := filepath.Join(dir, "template_templ.go")
	r.overlay[overlayPath] = []byte(overlayContent)

	// Set up dependency graph
	r.depGraph = newDependencyGraph()
	r.depGraph.addPackage("github.com/a-h/templ/generator/test-element-component", dir, true)
	r.depGraph.addTemplFile("github.com/a-h/templ/generator/test-element-component", filepath.Join(dir, "template.templ"))
	r.depGraph.buildInternalPackages()

	// Test resolveStructMethod
	sig, ok := r.resolveStructMethod("structComp.Page", dir)
	if !ok {
		t.Error("Failed to resolve structComp.Page")
	} else {
		t.Logf("Resolved structComp.Page: %+v", sig)
		t.Logf("Parameters: %+v", sig.parameters)
	}
}
