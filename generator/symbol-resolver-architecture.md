# Symbol Resolver Architecture

This document provides an in-depth analysis of the symbol resolution system in the templ codebase, focusing on `generator/symbol_resolver.go` and its usage patterns.

## Overview

The symbol resolver is the central component for type resolution in templ, enabling proper type checking and code generation for templ templates. It leverages Go's type system through overlays instead of string-based parsing. The system has been unified into a single resolver that combines component, variable, and expression resolution capabilities.

It should be used in the following way:

1. at the start of the templ generation process in `../cmd/templ/generatecmd/cmd.go`, we want to add a preprocessing step that walks all the files needs generation, symbol resolver should receive these files.
2. parse all the files and find out which ones require type analysis or symbols resolution, for a start, let's use all of them.
3. sort the modules topologically into a dependency graph
4. use `golang.org/x/tools/go/packages` to load the packages
