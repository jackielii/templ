package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

func TestBasicElementComponent(t *testing.T) {
	template := `package test

templ Component() {
	<div>content</div>
}

templ Test() {
	<Component />
}`

	tf, err := parser.ParseString(template)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	tf.Filepath = "/test/test.templ"

	var buf bytes.Buffer
	_, err = Generate(tf, &buf, WithFileName("/test/test.templ"))
	if err != nil {
		t.Fatalf("failed to generate: %v", err)
	}

	generated := buf.String()
	
	// Check that Component() is called
	if !strings.Contains(generated, "Component().Render(ctx, templ_7745c5c3_Buffer)") {
		t.Errorf("expected Component().Render call, got:\n%s", generated)
	}
}