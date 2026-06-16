package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/a-h/templ/parser/v2"
)

// generate parses templ source and returns the generated Go.
func generate(t *testing.T, src string) string {
	t.Helper()
	tf, err := parser.ParseString(src)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var b bytes.Buffer
	if _, err := Generate(tf, &b); err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	return b.String()
}

func TestHoistInComponentArg(t *testing.T) {
	src := `package test

templ Link(href string) {
	<a href={ href }>link</a>
}

templ Page() {
	@Link(templ.Hoist(url()))
}

func url() (string, error) {
	return "/x", nil
}
`
	out := generate(t, src)

	// The lifted call is assigned to a temp with the standard error variable and
	// an error handler is emitted before the component call.
	if !strings.Contains(out, "templ_7745c5c3_Var") || !strings.Contains(out, ", templ_7745c5c3_Err := url()") {
		t.Errorf("expected hoisted assignment of url() to a temp var; got:\n%s", out)
	}
	// The Hoist(...) wrapper must be gone from the component call, replaced by the temp.
	if strings.Contains(out, "templ.Hoist(") {
		t.Errorf("expected templ.Hoist(...) to be rewritten away; got:\n%s", out)
	}
	if !strings.Contains(out, "Link(templ_7745c5c3_Var") {
		t.Errorf("expected component call to use the temp var; got:\n%s", out)
	}
}

func TestHoistInPropsField(t *testing.T) {
	src := `package test

type Props struct {
	Href string
}

templ Button(p Props) {
	<a href={ p.Href }>btn</a>
}

templ Page() {
	@Button(Props{Href: templ.Hoist(url())})
}

func url() (string, error) {
	return "/x", nil
}
`
	out := generate(t, src)
	if !strings.Contains(out, ", templ_7745c5c3_Err := url()") {
		t.Errorf("expected url() hoisted; got:\n%s", out)
	}
	if !strings.Contains(out, "Props{Href: templ_7745c5c3_Var") {
		t.Errorf("expected Props field to reference the temp var; got:\n%s", out)
	}
}

func TestHoistReportsPreciseLocation(t *testing.T) {
	// Two hoists on different lines/cols inside one component call must each
	// report their own location — the entire reason error-return beats a
	// panicking must() helper.
	src := `package test

type Props struct {
	Href       string
	Attributes templ.Attributes
}

templ button(p Props) {
	<button { p.Attributes... }></button>
}

templ Page() {
	@button(Props{
		Href: templ.Hoist(href()),
		Attributes: templ.Attributes{
			"hx-post": templ.Hoist(post()),
		},
	})
}

func href() (string, error) { return "", nil }
func post() (string, error) { return "", nil }
`
	out := generate(t, src)
	// href() is on line 14; post() is on line 16 (1-based, as displayed).
	if !strings.Contains(out, "Line: 14") {
		t.Errorf("expected hoisted href() error to report Line: 14; got:\n%s", out)
	}
	if !strings.Contains(out, "Line: 16") {
		t.Errorf("expected hoisted post() error to report Line: 16; got:\n%s", out)
	}
	// The two error sites must have distinct locations, not a shared
	// component-call location.
	if strings.Count(out, "Line: 14") == 0 || strings.Count(out, "Line: 16") == 0 {
		t.Errorf("expected two distinct per-hoist locations; got:\n%s", out)
	}
}

func TestNoHoistWhenAbsent(t *testing.T) {
	src := `package test

templ Link(href string) {
	<a href={ href }>link</a>
}

templ Page() {
	@Link("/static")
}
`
	out := generate(t, src)
	if strings.Contains(out, "templ_7745c5c3_Err := ") {
		t.Errorf("did not expect any hoist statements; got:\n%s", out)
	}
}
