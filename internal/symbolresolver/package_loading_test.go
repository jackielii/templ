package symbolresolver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreprocessFiles_PackageLoading(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create test files in different packages
	testFiles := map[string]string{
		"pkg1/test.templ": `package pkg1

import "fmt"

templ Component() {
	<div>{ fmt.Sprint("Hello") }</div>
}`,
		"pkg2/test.templ": `package pkg2

templ Button(text string) {
	<button>{ text }</button>
}`,
		"pkg3/nested/test.templ": `package nested

import "testmod/pkg1"

templ Layout() {
	@pkg1.Component()
}`,
	}

	// Create the test files
	var files []string
	for path, content := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", fullPath, err)
		}
		files = append(files, fullPath)
	}

	// Create go.mod in the temp directory to establish module context
	goModContent := `module testmod

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	// Create the resolver and preprocess files
	resolver := NewSymbolResolverV2()
	err := resolver.PreprocessFiles(files)
	if err != nil {
		t.Fatalf("PreprocessFiles failed: %v", err)
	}

	// Verify overlays were created
	for _, file := range files {
		overlayPath := file[:len(file)-6] + "_templ.go" // Replace .templ with _templ.go
		if _, exists := resolver.overlays[overlayPath]; !exists {
			t.Errorf("Expected overlay for %s but it was not created", overlayPath)
		}
	}

	// Verify packages were loaded
	expectedPackages := []string{
		filepath.Join(tmpDir, "pkg1"),
		filepath.Join(tmpDir, "pkg2"),
		filepath.Join(tmpDir, "pkg3", "nested"),
	}

	for _, pkgDir := range expectedPackages {
		if _, exists := resolver.packages[pkgDir]; !exists {
			t.Errorf("Expected package for directory %s but it was not loaded", pkgDir)
		}
	}

	// Log what packages were actually loaded for debugging
	t.Logf("Loaded %d packages:", len(resolver.packages))
	for key, pkg := range resolver.packages {
		t.Logf("  Key: %s, PkgPath: %s", key, pkg.PkgPath)
	}
}