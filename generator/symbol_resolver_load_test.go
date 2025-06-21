package generator

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestOverlayPackageLoading(t *testing.T) {
	// Create a simple overlay content similar to what we generate
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

func Button(title string) templ.Component {
	return templ.NopComponent
}

func (s StructComponent) Page(title string, attrs templ.Attributer) templ.Component {
	return templ.NopComponent
}
`

	// Create overlay map
	overlay := map[string][]byte{
		"/tmp/test_templ.go": []byte(overlayContent),
	}

	// Configure package loading with overlay
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedImports,
		Dir:     "/tmp",
		Overlay: overlay,
	}

	// Load the package
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatalf("Failed to load package: %v", err)
	}

	if len(pkgs) == 0 {
		t.Fatal("No packages loaded")
	}

	pkg := pkgs[0]

	// Check for errors (ignoring import errors)
	var nonImportErrors []string
	for _, err := range pkg.Errors {
		errStr := err.Error()
		if !strings.Contains(errStr, "imported and not used") && !strings.Contains(errStr, "imported as") {
			nonImportErrors = append(nonImportErrors, errStr)
		}
	}

	if len(nonImportErrors) > 0 {
		t.Logf("Package errors: %v", nonImportErrors)
	}

	// Check if Types is available
	if pkg.Types == nil {
		t.Fatal("Package Types is nil")
	}

	// Check if Scope is available
	if pkg.Types.Scope() == nil {
		t.Fatal("Package Types.Scope() is nil")
	}

	// List all names in scope
	names := pkg.Types.Scope().Names()
	t.Logf("Names in scope: %v", names)

	// Look for structComp variable
	obj := pkg.Types.Scope().Lookup("structComp")
	if obj == nil {
		t.Error("Variable 'structComp' not found in package scope")

		// Print what we do have
		t.Logf("Available objects in scope:")
		for _, name := range names {
			obj := pkg.Types.Scope().Lookup(name)
			t.Logf("  %s: %T", name, obj)
		}
	} else {
		t.Logf("Found structComp: %T", obj)
	}

	// Look for StructComponent type
	typeObj := pkg.Types.Scope().Lookup("StructComponent")
	if typeObj == nil {
		t.Error("Type 'StructComponent' not found in package scope")
	} else {
		t.Logf("Found StructComponent: %T", typeObj)
	}

	// Look for Page method
	if typeObj != nil {
		// This would require more complex type analysis, but we can at least verify the type exists
		t.Logf("StructComponent type found successfully")
	}
}
