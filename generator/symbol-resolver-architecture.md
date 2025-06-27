# Symbol Resolver Architecture

This document provides an in-depth analysis of the symbol resolution system in the templ codebase, focusing on `generator/symbol_resolver.go` and its usage patterns.

## Overview

The symbol resolver is the central component for type resolution in templ, enabling proper type checking and code generation for templ templates. It leverages Go's type system through overlays instead of string-based parsing. The system has been unified into a single resolver that combines component, variable, and expression resolution capabilities.

It should be used in the following way:

1. at the start of the templ generation process in `../cmd/templ/generatecmd/cmd.go`, we want to add a preprocessing step that walks all the files needs generation, symbol resolver should receive these files.
2. parse all the files and find out which ones require type analysis or symbols resolution, for a start, let's use all of them.
3. sort the modules topologically into a dependency graph
4. use `golang.org/x/tools/go/packages` to load the packages

### Element component

Element component use the HTML element syntax but it accepts any valid Go expression as it's tag name. We want to support the following variants:

1. `<Button>Click me</Button>` - A `func Button(...) templ.Component` function that returns a templ.Component
2. `<mycomponent>Click me</mycomponent>` - A `func mycomponent(...) templ.Component` function that returns a templ.Component
3. `<p.mycomponent>Click me</p.mycomponent>` - A `func (p *mystruct) mycomponent(...) templ.Component` function that returns a templ.Component.
4. `<pkg.Custom>Click me</pkg.Custom>` - A `func Custom(...) templ.Component` function from external package `pkg` that returns a templ.Component, where `pkg` is a package or package alias.
5. `<myValue>...</myValue>` - A value of a type that implements the `templ.Component` interface. I.e. has a method `func (s *myType) Render(context.Context, w io.Writer) error`. In addition, if this value is a struct value, we also support populating its fields with the attributes.
