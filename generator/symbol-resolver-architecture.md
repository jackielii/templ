# Symbol Resolver Architecture

This document provides an in-depth analysis of the symbol resolution system in the templ codebase, focusing on `generator/symbol_resolver.go` and its usage patterns.

## Overview

The symbol resolver is the central component for type resolution in templ, enabling proper type checking and code generation for templ templates. It leverages Go's type system through overlays instead of string-based parsing. The system has been unified into a single resolver that combines component, variable, and expression resolution capabilities.

## Core Design Principles

### 1. Overlay-based Resolution
- Generates Go stub files (`_templ.go`) that make templ files the single source of truth
- Uses `golang.org/x/tools/go/packages` with overlays for type resolution
- Overlays contain:
  - All imports and top-level Go code from templ files
  - Function stubs for templ components returning `templ.Component`
  - CSS templates returning `templ.CSSClass`
  - Script templates returning `templ.ComponentScript`

### 2. Lazy Package Loading
- Packages are loaded on-demand with full type information
- Automatic processing of all templ files in a directory when loading a package
- Caches loaded packages to avoid redundant processing

### 3. Context-aware Resolution
- Tracks AST position during code generation via `GeneratorContext`
- Maintains scope stack for local variables (for loops, if statements)
- Resolves symbols based on current context (template parameters, local vars, package scope)

## Key Components

### componentSignature (Unified)
Represents a component's type information (now unexported):
```go
type componentSignature struct {
    name       string
    parameters []parameterInfo
    isStruct   bool
    isCached   bool
}
```

### parameterInfo (Unified)
Rich type information for parameters/fields (now unexported):
```go
type parameterInfo struct {
    name         string
    typ          string // String representation
    isComponent  bool   // Implements templ.Component
    isAttributer bool   // Implements templ.Attributer
    isPointer    bool
    isSlice      bool
    isMap        bool
    isString     bool
    isBool       bool
}
```

### generatorContext (Unified)
Tracks position in AST during generation with enhanced scope management:
```go
type generatorContext struct {
    currentTemplate *parser.HTMLTemplate
    scopes         []localScope
}

type localScope struct {
    node      parser.Node
    variables map[string]*symbolTypeInfo
}
```

## Main Entry Points (Unified System)

### 1. resolveComponent (Internal)
Primary method for resolving components (replaces ResolveElementComponent):
```go
func (r *symbolResolver) resolveComponent(
    fromDir, currentPkg string,
    componentName string
) (componentSignature, error)
```

**Resolution strategies:**
- Simple names: Local component via template cache
- Dotted names: Delegate to resolveExpression for full type analysis
- Automatic module directory detection via modcheck.WalkUp

### 2. resolveExpression (Enhanced)
Determines type information for expressions with full context awareness:
```go
func (r *symbolResolver) resolveExpression(
    expr string,
    ctx *generatorContext,
    fromDir string
) (*symbolTypeInfo, error)
```

**Resolution order:**
1. Local scopes (innermost to outermost) - includes for/if variables
2. Template parameters (if inside a template)
3. Package-level symbols via packages.Load
4. Method resolution on struct types
5. Field access resolution

### 3. Unified Resolution System
The unified resolver combines:
- **Template caching**: Local templates cached for fast lookup
- **Expression resolution**: Full Go expression type analysis
- **Variable extraction**: Automatic extraction of for/if variables
- **Context tracking**: Proper scope management during generation

## Usage Patterns

### In Element Component Generation
From `generator_element_component.go`:

1. **Component Resolution** (simplified):
```go
sig, err := g.symbolResolver.resolveComponent(
    g.currentFileDir(), 
    currentPkgPath, 
    n.Name
)
```

2. **Expression Type Checking** (enhanced context):
```go
typeInfo, err := g.symbolResolver.resolveExpression(
    exprValue, 
    g.context, 
    g.currentFileDir()
)
if err == nil && typeInfo.isComponent {
    // Expression is definitely a component
}
```

### Component Type Handling

The resolver distinguishes between:
1. **Function Components**: Regular functions returning `templ.Component`
2. **Struct Components**: Types implementing the `templ.Component` interface
3. **Method Components**: Methods on struct variables that return components

## Implementation Details

### Unified Resolver Architecture
The unified `symbolResolver` combines multiple resolution strategies:

1. **Template Caching**: All local templates cached during initialization
2. **Expression Analysis**: Full Go expression parsing with AST
3. **Context Management**: Proper scope tracking with variable extraction
4. **Module Detection**: Automatic go.mod detection via modcheck.WalkUp

### Type Analysis
Three levels of type analysis:
1. **Template-based**: Direct lookup from cached templ components
2. **AST-based**: Expression parsing for variable/field access
3. **Full type checking** (via packages.Load): Complete type information including interface satisfaction

### Caching Strategy
- Local templates cached at resolver initialization
- Package information cached by import path
- Type information cached with full expression analysis
- Automatic module directory detection and caching

## Error Handling

The unified resolver provides enhanced error handling:
- Context-aware error messages with proper scope information
- Graceful fallback for missing components
- Automatic module boundary detection
- Clear distinction between local and cross-package resolution failures

## Performance Considerations

1. **Upfront Template Caching**: All local templates cached at initialization
2. **Lazy Package Loading**: External packages loaded only when needed
3. **Expression Caching**: Parsed expressions cached for reuse
4. **Module Detection**: go.mod location cached per directory
5. **Minimal AST Parsing**: Only parse what's needed for resolution

## Key Improvements in Unified System

### 1. Variable Extraction
Automatic extraction of variables from control flow:
- **For loops**: Extract index and value variables with types
- **If statements**: Extract short variable declarations
- **Proper scoping**: Variables only visible within their scope

### 2. Context-Aware Resolution
- Full AST path tracking during generation
- Nested scope support for complex templates
- Template parameter injection into scope

### 3. Expression Parsing
Enhanced expression analysis:
- Method calls on struct variables
- Field access resolution
- Slice/map indexing support
- Type assertion handling

### 4. Module Boundary Detection
- Automatic go.mod detection via modcheck.WalkUp
- Proper package resolution across module boundaries
- Cached module locations for performance

## Future Enhancement Opportunities

### 1. Generic Type Support
- Track generic type parameters through expressions
- Support type inference for generic components

### 2. Advanced Control Flow
- Switch statement type narrowing
- Range over map key/value type extraction
- Select statement channel type analysis

### 3. Performance Optimizations
- Parallel package loading for multiple imports
- Incremental cache updates for file changes
- AST node reuse for common patterns

## Integration Points

The unified symbol resolver integrates with:
1. **Code Generator**: Provides type information with full context awareness
2. **Parser**: Receives AST nodes for analysis and variable extraction
3. **LSP**: Enhanced diagnostics with proper type resolution
4. **Formatter**: Skip resolution option for performance when not needed
5. **Import Processing**: Automatic module detection for proper resolution

## Conclusion

The unified symbol resolver represents a significant evolution in templ's type resolution capabilities. By combining template caching, expression analysis, and context-aware variable tracking into a single cohesive system, it provides:

- **Better Developer Experience**: Accurate type information throughout templates
- **Enhanced Safety**: Proper variable scoping and type checking
- **Improved Performance**: Strategic caching and lazy loading
- **Cleaner Architecture**: Single resolver instead of multiple systems

The system maintains Go's type safety while providing the flexibility needed for a template language, making it suitable for both CLI generation and IDE integration scenarios.