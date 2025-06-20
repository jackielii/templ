package generator

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

func TestFullGenerationWithStructMethod(t *testing.T) {
	// Parse the actual template file
	templatePath := filepath.Join("test-element-component", "template.templ")
	tf, err := parser.Parse(templatePath)
	if err != nil {
		t.Fatalf("Failed to parse template: %v", err)
	}
	
	// Simulate preprocessing
	templFiles := []string{templatePath}
	preprocessResult, err := PreprocessTemplFiles("test-element-component", templFiles)
	if err != nil {
		t.Fatalf("Failed to preprocess: %v", err)
	}
	
	t.Logf("Preprocessing result: ElementComponentsDetected=%v, InternalPackages=%d", 
		preprocessResult.ElementComponentsDetected, 
		len(preprocessResult.GetInternalPackages()))
	
	// Now try to generate
	var buf bytes.Buffer
	opts := []GenerateOpt{
		WithFileName(templatePath),
	}
	
	output, err := Generate(tf, &buf, opts...)
	if err != nil {
		t.Errorf("Generation failed: %v", err)
	}
	
	// Check if the generated code contains structComp.Page
	generated := buf.String()
	if !bytes.Contains([]byte(generated), []byte("structComp.Page")) {
		t.Error("Generated code should reference structComp.Page")
	}
	
	t.Logf("Generated %d bytes", len(generated))
	if len(output.Diagnostics) > 0 {
		t.Logf("Diagnostics: %+v", output.Diagnostics)
	}
}