package generator

import (
	"strings"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

// TestGeneratorWithUnifiedResolver tests the generator with the new unified resolver
func TestGeneratorWithUnifiedResolver(t *testing.T) {
	// Template that uses an element component from another package
	templContent := `package main

import "github.com/a-h/templ/generator/test-element-component/mod"

templ TestPage() {
	<mod.Text name="test" attrs={templ.Attributes{"class": "test"}} />
}
`

	// Parse the template
	tf, err := parser.ParseString(templContent)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Generate code with unified resolver (automatically enabled)
	var output strings.Builder
	opts := []GenerateOpt{
		WithFileName("./test-element-component/test.templ"),
	}

	result, err := Generate(tf, &output, opts...)
	if err != nil {
		t.Fatalf("failed to generate code: %v", err)
	}

	generatedCode := output.String()

	// Verify the generated code contains the component call
	if !strings.Contains(generatedCode, "mod.Text") {
		t.Error("generated code should contain component call to mod.Text")
	}

	// Verify no diagnostics (errors) were generated
	if len(result.Diagnostics) > 0 {
		t.Errorf("unexpected diagnostics: %v", result.Diagnostics)
	}

	t.Logf("Generated code length: %d characters", len(generatedCode))
	t.Logf("Component signatures resolved: %d", len(result.Options.FileName))
}

// TestGeneratorWithLocalComponent tests local component resolution
func TestGeneratorWithLocalComponent(t *testing.T) {
	// Template with local components
	templContent := `package main

templ Button(title string) {
	<button>{ title }</button>
}

templ TestPage() {
	@Button("Click me")
}
`

	// Parse the template
	tf, err := parser.ParseString(templContent)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Generate code with unified resolver (automatically enabled)
	var output strings.Builder
	opts := []GenerateOpt{
		WithFileName("./test-element-component/test2.templ"),
	}

	result, err := Generate(tf, &output, opts...)
	if err != nil {
		t.Fatalf("failed to generate code: %v", err)
	}

	generatedCode := output.String()

	// Verify the generated code contains both components
	if !strings.Contains(generatedCode, "func Button(") {
		t.Error("generated code should contain Button function definition")
	}

	if !strings.Contains(generatedCode, "func TestPage(") {
		t.Error("generated code should contain TestPage function definition")
	}

	// Verify no diagnostics (errors) were generated
	if len(result.Diagnostics) > 0 {
		t.Errorf("unexpected diagnostics: %v", result.Diagnostics)
	}

	t.Logf("Generated code length: %d characters", len(generatedCode))
}
