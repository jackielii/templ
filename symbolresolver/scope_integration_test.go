package symbolresolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

func TestScopeAssignmentIntegration(t *testing.T) {
	// Find a test template file in the examples directory
	examplesDir := filepath.Join("..", "examples")
	if _, err := os.Stat(examplesDir); os.IsNotExist(err) {
		t.Skip("examples directory not found")
	}
	
	// Look for template files in examples
	var templFile string
	err := filepath.Walk(examplesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if filepath.Ext(path) == ".templ" {
			templFile = path
			return filepath.SkipDir // Stop after finding the first one
		}
		return nil
	})
	
	if err != nil {
		t.Fatalf("failed to walk examples directory: %v", err)
	}
	
	if templFile == "" {
		t.Skip("no .templ files found in examples directory")
	}
	
	// Parse the template
	tf, err := parser.Parse(templFile)
	if err != nil {
		t.Fatalf("failed to parse template %s: %v", templFile, err)
	}
	tf.Filepath = templFile
	
	// Create resolver and preprocess the file
	resolver := NewSymbolResolverV2()
	if err := resolver.PreprocessFiles([]string{templFile}); err != nil {
		t.Fatalf("failed to preprocess files: %v", err)
	}
	
	// Check if any packages were loaded
	if len(resolver.packages) == 0 {
		t.Skip("No packages loaded - skipping integration test")
	}
	
	// Try to assign scopes
	if err := resolver.AssignScopes(tf); err != nil {
		t.Fatalf("failed to assign scopes: %v", err)
	}
	
	// Basic verification - check if the template file has a scope
	if tf.Scope() == nil {
		t.Error("expected template file to have scope")
	}
	
	// Find the first HTMLTemplate and check if it has a scope
	var htmlTemplate *parser.HTMLTemplate
	for _, node := range tf.Nodes {
		if ht, ok := node.(*parser.HTMLTemplate); ok {
			htmlTemplate = ht
			break
		}
	}
	
	if htmlTemplate != nil && htmlTemplate.Scope() == nil {
		t.Error("expected HTMLTemplate to have scope")
	}
	
	t.Logf("Successfully assigned scopes to template %s", templFile)
}