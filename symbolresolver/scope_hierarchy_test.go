package symbolresolver

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func TestScopeHierarchyWithImports(t *testing.T) {
	// Create a Go program with imports
	src := `package main

import (
	"fmt"
	myfmt "fmt"
)

var pkgVar = 42

func main() {
	fmt.Println(pkgVar)
	myfmt.Println("hello")
}`

	// Parse the file
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Type check with proper importer
	conf := types.Config{
		Importer: importer.Default(),
	}
	info := &types.Info{
		Scopes: make(map[ast.Node]*types.Scope),
	}
	pkg, err := conf.Check("main", fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatal(err)
	}

	// Get file scope
	fileScope := info.Scopes[file]
	if fileScope == nil {
		t.Fatal("file scope not found")
	}

	// Get package scope
	pkgScope := pkg.Scope()

	// Check parent relationship
	t.Logf("File scope names: %v", fileScope.Names())
	t.Logf("File scope parent: %v", fileScope.Parent())
	t.Logf("Package scope: %v", pkgScope)
	t.Logf("Package scope names: %v", pkgScope.Names())

	// Verify that file scope's parent is the package scope
	if fileScope.Parent() != pkgScope {
		t.Errorf("Expected file scope parent to be package scope, got %v", fileScope.Parent())
	}

	// Test lookup behavior
	// File scope should only contain imports
	if obj := fileScope.Lookup("fmt"); obj == nil {
		t.Error("Expected to find fmt in file scope")
	}
	if obj := fileScope.Lookup("myfmt"); obj == nil {
		t.Error("Expected to find myfmt in file scope")
	}
	if obj := fileScope.Lookup("pkgVar"); obj != nil {
		t.Error("Did not expect to find pkgVar in file scope")
	}

	// Package scope should contain package-level declarations
	if obj := pkgScope.Lookup("pkgVar"); obj == nil {
		t.Error("Expected to find pkgVar in package scope")
	}
	if obj := pkgScope.Lookup("main"); obj == nil {
		t.Error("Expected to find main in package scope")
	}

	// LookupParent should find pkgVar when starting from file scope
	scope, obj := fileScope.LookupParent("pkgVar", token.NoPos)
	if obj == nil {
		t.Error("Expected LookupParent to find pkgVar")
	}
	if scope != pkgScope {
		t.Error("Expected LookupParent to return package scope")
	}

	// Test with ResolveExpression - it should work with just file scope
	// because file scope has package scope as parent
	exprStr := "pkgVar"
	expr, err := parser.ParseExpr(exprStr)
	if err != nil {
		t.Fatal(err)
	}

	// Try with just file scope (it has package scope as parent)
	typ, err := ResolveExpression(expr, fileScope)
	if err != nil {
		t.Errorf("ResolveExpression failed: %v", err)
	}
	if typ.String() != "int" {
		t.Errorf("Expected int type, got %s", typ)
	}

	// Also test that it works with package scope directly for non-import identifiers
	typ2, err := ResolveExpression(expr, pkgScope)
	if err != nil {
		t.Errorf("ResolveExpression failed with package scope: %v", err)
	}
	if typ2.String() != "int" {
		t.Errorf("Expected int type, got %s", typ2)
	}

	// Now test what happens if we use LookupParent in ResolveExpression
	// This would be more correct as it would search up the hierarchy
	t.Log("Testing with LookupParent approach...")
}
