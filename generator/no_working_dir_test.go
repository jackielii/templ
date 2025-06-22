package generator

import (
	"strings"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

// TestNoWorkingDirNeeded verifies that the generator works without explicit working directory setup
func TestNoWorkingDirNeeded(t *testing.T) {
	// Template that uses a cross-package component
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

	// Generate code without any WithWorkingDir option - should work automatically
	var output strings.Builder
	opts := []GenerateOpt{
		WithFileName("./test-element-component/test.templ"),
	}

	result, err := Generate(tf, &output, opts...)
	if err != nil {
		t.Fatalf("failed to generate code without working dir: %v", err)
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

	t.Logf("Successfully generated %d characters without WithWorkingDir", len(generatedCode))
}

// TestSimpleTemplateGeneration verifies basic template generation without cross-package dependencies
func TestSimpleTemplateGeneration(t *testing.T) {
	// Simple template with no cross-package dependencies
	templContent := `package main

templ Hello(name string) {
	<h1>Hello, { name }!</h1>
}

templ Page() {
	@Hello("World")
}
`

	// Parse the template
	tf, err := parser.ParseString(templContent)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Generate code with no options at all
	var output strings.Builder
	result, err := Generate(tf, &output)
	if err != nil {
		t.Fatalf("failed to generate simple template: %v", err)
	}

	generatedCode := output.String()

	// Verify the generated code contains both functions
	if !strings.Contains(generatedCode, "func Hello(") {
		t.Error("generated code should contain Hello function")
	}

	if !strings.Contains(generatedCode, "func Page(") {
		t.Error("generated code should contain Page function")
	}

	// Verify no diagnostics (errors) were generated
	if len(result.Diagnostics) > 0 {
		t.Errorf("unexpected diagnostics: %v", result.Diagnostics)
	}

	t.Logf("Successfully generated simple template: %d characters", len(generatedCode))
}

// TestAutomaticModuleDetection verifies that module detection works from different starting directories
func TestAutomaticModuleDetection(t *testing.T) {
	// Template content that uses local components
	templContent := `package main

templ Button(title string) {
	<button>{ title }</button>
}

templ Page() {
	@Button("Click me")
}
`

	// Parse the template
	tf, err := parser.ParseString(templContent)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Test generation from different file paths to verify module detection works
	testCases := []struct {
		name     string
		fileName string
	}{
		{
			name:     "From subdirectory",
			fileName: "./test-element-component/subdir/test.templ",
		},
		{
			name:     "From root",
			fileName: "./test.templ",
		},
		{
			name:     "From nested path",
			fileName: "./test-element-component/mod/test.templ",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var output strings.Builder
			opts := []GenerateOpt{
				WithFileName(tc.fileName),
			}

			_, err := Generate(tf, &output, opts...)
			if err != nil {
				t.Fatalf("failed to generate from %s: %v", tc.fileName, err)
			}

			generatedCode := output.String()
			if len(generatedCode) == 0 {
				t.Error("generated code should not be empty")
			}

			// Basic check that it generated valid Go code
			if !strings.Contains(generatedCode, "func Button(") {
				t.Error("generated code should contain Button function")
			}

			t.Logf("Generated from %s: %d characters", tc.fileName, len(generatedCode))
		})
	}
}

// TestEnhancedDiagnosticsWithoutWorkingDir verifies enhanced diagnostics work without working dir
func TestEnhancedDiagnosticsWithoutWorkingDir(t *testing.T) {
	// Template with a missing component
	templContent := `package main

templ Page() {
	<NonExistentComponent test="value" />
}
`

	// Parse the template
	tf, err := parser.ParseString(templContent)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	// Test enhanced diagnostics without providing working directory
	diags, err := DiagnoseWithSymbolResolution(tf)
	if err != nil {
		t.Fatalf("enhanced diagnostics failed: %v", err)
	}

	// Should have at least one diagnostic for the missing component
	if len(diags) == 0 {
		t.Error("expected diagnostic for missing component")
	}

	t.Logf("Enhanced diagnostics found %d issues", len(diags))
	for i, diag := range diags {
		t.Logf("  %d: %s", i, diag.Message)
	}
}
