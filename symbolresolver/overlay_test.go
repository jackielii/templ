package symbolresolver

import (
	"testing"

	"github.com/a-h/templ/parser/v2"
	"github.com/google/go-cmp/cmp"
)

func TestGenerateOverlay(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "const declaration",
			content: `package test

const foo = "bar"

templ Component() {
	<div>{ foo }</div>
}`,
			want: `package test

import "github.com/a-h/templ"

const foo = "bar"

func Component() templ.Component {
	return templ.NopComponent
}

`,
		},
		{
			name: "var declaration",
			content: `package test

var count = 42

templ Component() {
	<div>{ fmt.Sprint(count) }</div>
}`,
			want: `package test

import "github.com/a-h/templ"

var count = 42

func Component() templ.Component {
	return templ.NopComponent
}

`,
		},
		{
			name: "multiple const declarations",
			content: `package test

const (
	a = 1
	b = 2
)

templ Component() {
	<div>{ fmt.Sprint(a + b) }</div>
}`,
			want: `package test

import "github.com/a-h/templ"

const (
	a = 1
	b = 2
)

func Component() templ.Component {
	return templ.NopComponent
}

`,
		},
		{
			name: "multiple var declarations",
			content: `package test

var (
	x int
	y string = "hello"
)

templ Component() {
	<div>{ y }</div>
}`,
			want: `package test

import "github.com/a-h/templ"

var (
	x int
	y string = "hello"
)

func Component() templ.Component {
	return templ.NopComponent
}

`,
		},
		{
			name: "mixed declarations with comments",
			content: `package test

import "fmt"

// This is a comment about constants
const prefix = "MSG: "

// Global variables
var (
	// Counter tracks the number of messages
	counter int
)

type Message struct {
	Text string
}

func helper() string {
	return "helper"
}

templ Component(msg Message) {
	<div>{ prefix + msg.Text }</div>
}`,
			want: `package test

import (
	"github.com/a-h/templ"
	"fmt"
)

const prefix = "MSG: "

var (
	// Counter tracks the number of messages
	counter int
)

type Message struct {
	Text string
}

func helper() string {
	return "helper"
}

func Component(msg Message) templ.Component {
	return templ.NopComponent
}

`,
		},
		{
			name: "nested scopes with local variables",
			content: `package main

import "fmt"

templ ComplexScopes(data []int) {
	{{ total := 0 }}
	
	for i, val := range data {
		{{ doubled := val * 2 }}
		if doubled > 10 {
			{{ msg := fmt.Sprintf("Large: %d", doubled) }}
			<p>{ msg }</p>
		} else {
			{{ msg := "Small value" }}
			<span>{ msg }</span>
		}
		{{ total = total + val }}
	}
	
	switch {
	case total < 10:
		{{ result := "Low total" }}
		<div>{ result }</div>
	case total < 100:
		{{ result := "Medium total" }}
		<div>{ result }</div>
	default:
		{{ result := fmt.Sprintf("High total: %d", total) }}
		<div>{ result }</div>
	}
}`,
			want: `package main

import (
	"github.com/a-h/templ"
	"fmt"
)

func ComplexScopes(data []int) templ.Component {
	total := 0
	for i, val := range data {
		doubled := val * 2
		if doubled > 10 {
			msg := fmt.Sprintf("Large: %d", doubled)
		} else {
			msg := "Small value"
		}
		total = total + val
	}
	switch  {
	case total < 10:
		result := "Low total"
	case total < 100:
		result := "Medium total"
	default:
		result := fmt.Sprintf("High total: %d", total)
	}
	return templ.NopComponent
}

`,
		},
		{
			name: "short variable declaration with comma-ok",
			content: `package main

templ CheckMap(data map[string]int) {
	if val, ok := data["key"]; ok {
		<p>Found: { fmt.Sprint(val) }</p>
	}
	
	// Type assertion with comma-ok
	{{ var iface interface{} = "test" }}
	if str, ok := iface.(string); ok {
		<p>String value: { str }</p>
	}
	
	// Channel receive with comma-ok
	{{ ch := make(chan int, 1) }}
	{{ ch <- 42 }}
	if num, ok := <-ch; ok {
		<p>Received: { fmt.Sprint(num) }</p>
	}
}`,
			want: `package main

import "github.com/a-h/templ"

func CheckMap(data map[string]int) templ.Component {
	if val, ok := data["key"]; ok {
	}
	var iface interface{} = "test"
	if str, ok := iface.(string); ok {
	}
	ch := make(chan int, 1)
	ch <- 42
	if num, ok := <-ch; ok {
	}
	return templ.NopComponent
}

`,
		},
		{
			name: "type switch",
			content: `package main

templ ProcessValue(val interface{}) {
	switch v := val.(type) {
	case string:
		{{ length := len(v) }}
		<p>String of length { fmt.Sprint(length) }</p>
	case int:
		{{ doubled := v * 2 }}
		<p>Int doubled: { fmt.Sprint(doubled) }</p>
	case []string:
		{{ count := len(v) }}
		<p>Array with { fmt.Sprint(count) } items</p>
		for i, s := range v {
			<li>{ fmt.Sprintf("%d: %s", i, s) }</li>
		}
	default:
		{{ typeName := fmt.Sprintf("%T", v) }}
		<p>Unknown type: { typeName }</p>
	}
}`,
			want: `package main

import "github.com/a-h/templ"

func ProcessValue(val interface{}) templ.Component {
	switch v := val.(type) {
	case string:
		length := len(v)
	case int:
		doubled := v * 2
	case []string:
		count := len(v)
		for i, s := range v {
		}
	default:
		typeName := fmt.Sprintf("%T", v)
	}
	return templ.NopComponent
}

`,
		},
		{
			name: "range with different patterns",
			content: `package main

templ RangePatterns(items []string, mapping map[string]int) {
	// Range over slice with index and value
	for i, item := range items {
		<div>{ fmt.Sprintf("%d: %s", i, item) }</div>
	}
	
	// Range over slice with only index
	for i := range items {
		<div>Index: { fmt.Sprint(i) }</div>
	}
	
	// Range over map
	for key, value := range mapping {
		<div>{ key }: { fmt.Sprint(value) }</div>
	}
	
	// Range with blank identifier
	for _, item := range items {
		<span>{ item }</span>
	}
	
	// Range over channel
	{{ ch := make(chan string, 3) }}
	{{ ch <- "one" }}
	{{ ch <- "two" }}
	{{ ch <- "three" }}
	{{ close(ch) }}
	for msg := range ch {
		<p>{ msg }</p>
	}
}`,
			want: `package main

import "github.com/a-h/templ"

func RangePatterns(items []string, mapping map[string]int) templ.Component {
	for i, item := range items {
	}
	for i := range items {
	}
	for key, value := range mapping {
	}
	for _, item := range items {
	}
	ch := make(chan string, 3)
	ch <- "one"
	ch <- "two"
	ch <- "three"
	close(ch)
	for msg := range ch {
	}
	return templ.NopComponent
}

`,
		},
		{
			name: "nested if with short declarations",
			content: `package main

templ NestedConditions(users []User) {
	if len(users) > 0 {
		{{ first := users[0] }}
		if name := first.Name; name != "" {
			{{ greeting := "Hello, " + name }}
			<h1>{ greeting }</h1>
			
			if age := first.Age; age >= 18 {
				{{ status := "adult" }}
				<p>User is an { status }</p>
			} else if age > 0 {
				{{ status := "minor" }}
				<p>User is a { status }</p>
			}
		}
	}
}`,
			want: `package main

import "github.com/a-h/templ"

func NestedConditions(users []User) templ.Component {
	if len(users) > 0 {
		first := users[0]
		if name := first.Name; name != "" {
			greeting := "Hello, " + name
			if age := first.Age; age >= 18 {
				status := "adult"
			} else if age > 0 {
				status := "minor"
			}
		}
	}
	return templ.NopComponent
}

`,
		},
		{
			name: "multiple variable declarations",
			content: `package main

templ MultipleVars() {
	{{ 
		x := 1
		y := 2
		z := x + y
	}}
	<p>Sum: { fmt.Sprint(z) }</p>
	
	{{ a, b, c := 1, 2, 3 }}
	<p>Values: { fmt.Sprint(a, b, c) }</p>
	
	{{ var (
		name = "test"
		count = 42
	) }}
	<p>{ name }: { fmt.Sprint(count) }</p>
}`,
			want: `package main

import "github.com/a-h/templ"

func MultipleVars() templ.Component {
	
		x := 1
		y := 2
		z := x + y
	a, b, c := 1, 2, 3
	var (
		name = "test"
		count = 42
	)
	return templ.NopComponent
}

`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tf, err := parser.ParseString(tt.content)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			overlay, err := generateOverlay(tf)
			if err != nil {
				t.Fatalf("GenerateOverlay failed: %v", err)
			}

			if diff := cmp.Diff(tt.want, overlay); diff != "" {
				t.Errorf("Generated overlay mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
