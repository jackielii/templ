# Symbol Resolver Architecture

This document provides an in-depth analysis of the symbol resolution system in the templ codebase, focusing on `generator/symbol_resolver.go` and its usage patterns.

## Overview

The symbol resolver is the central component for type resolution in templ, enabling proper type checking and code generation for templ templates. It leverages Go's type system through overlays instead of string-based parsing.

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

### ComponentSignature
Represents a component's type information:
```go
type ComponentSignature struct {
    PackagePath   string
    Name          string
    QualifiedName string          // Fully qualified name for caching
    Parameters    []ParameterInfo // Function params or struct fields
    IsStruct      bool            // Whether it's a struct implementing Component
    IsPointerRecv bool            // For struct methods with pointer receivers
}
```

### ParameterInfo
Rich type information for parameters/fields:
```go
type ParameterInfo struct {
    Name         string
    Type         string // String representation
    IsComponent  bool   // Implements templ.Component
    IsAttributer bool   // Implements templ.Attributer
    IsPointer    bool
    IsSlice      bool
    IsMap        bool
    IsString     bool
    IsBool       bool
}
```

### GeneratorContext
Tracks position in AST during generation:
```go
type GeneratorContext struct {
    CurrentTemplate *parser.HTMLTemplate // Current template being generated
    ASTPath         []parser.Node        // Path from root to current node
    LocalScopes     []LocalScope         // Stack of local scopes
}
```

## Main Entry Points

### 1. ResolveElementComponent
Primary method for resolving components in element syntax:
```go
func (r *SymbolResolver) ResolveElementComponent(
    fromDir, currentPkg string, 
    componentName string, 
    tf *parser.TemplateFile
) (ComponentSignature, error)
```

**Resolution strategies:**
- Simple names: Local component in current package
- Dotted names with import alias: Cross-package component (e.g., `mod.Button`)
- Dotted names without alias: Struct method component (e.g., `myStruct.Render`)

### 2. ResolveExpression
Determines type information for expressions:
```go
func (r *SymbolResolver) ResolveExpression(
    expr string, 
    ctx GeneratorContext, 
    fromDir string
) (*TypeInfo, error)
```

**Resolution order:**
1. Local scopes (innermost to outermost)
2. Template parameters (if inside a template)
3. Package-level symbols

### 3. Register
Creates overlays for templ files:
```go
func (r *SymbolResolver) Register(
    tf *parser.TemplateFile, 
    fileName string
) error
```

## Usage Patterns

### In Element Component Generation
From `generator_element_component.go`:

1. **Component Resolution** (line 464):
```go
sigs, err := g.symbolResolver.ResolveElementComponent(
    g.currentFileDir(), 
    currentPkgPath, 
    n.Name, 
    g.tf
)
```

2. **Expression Type Checking** (line 177):
```go
typeInfo, err := g.symbolResolver.ResolveExpression(
    exprValue, 
    *g.context, 
    g.currentFileDir()
)
if err == nil && typeInfo.IsComponent {
    // Expression is definitely a component
}
```

### Component Type Handling

The resolver distinguishes between:
1. **Function Components**: Regular functions returning `templ.Component`
2. **Struct Components**: Types implementing the `templ.Component` interface
3. **Method Components**: Methods on struct variables that return components

## Implementation Details

### Overlay Generation
The `generateOverlay` method creates minimal Go code that's sufficient for type checking:
- Preserves all imports and top-level Go code
- Creates function stubs for templates
- Ensures templ-specific imports are present

### Type Analysis
Two levels of type analysis:
1. **AST-based** (for overlays): Basic type identification from syntax
2. **Full type checking** (via packages.Load): Complete type information including interface satisfaction

### Caching Strategy
- Component signatures cached by fully qualified name
- Package information cached by directory
- Overlays retained for the resolver's lifetime

## Error Handling

The resolver provides position-aware errors via `ComponentResolutionError`:
- Includes file name, line, and column information
- Gracefully handles packages with compilation errors in generated files
- Distinguishes between generated file errors (ignored) and source errors

## Performance Considerations

1. **Batch Processing**: `processTemplFiles` processes all templ files in a directory at once
2. **Lazy Loading**: Packages only loaded when needed
3. **Caching**: Extensive caching of packages and signatures
4. **Overlay Reuse**: Overlays generated once and reused

## Future Enhancement Opportunities

### 1. On-demand Overlay Generation
Instead of processing all templ files in a directory, generate overlays only for:
- The current file being processed
- Files that are imported/referenced

### 2. Incremental Updates
For IDE/LSP scenarios:
- Support updating individual overlays
- Track dependencies between files
- Minimal reprocessing on changes

### 3. Enhanced Context Tracking
- Type narrowing from type assertions
- Switch case type tracking
- Import alias resolution at context level

### 4. Parallel Resolution
- Batch multiple `ResolveElementComponent` calls
- Parallel package loading for cross-package components

### 5. Type Inference Improvements
- Method chain resolution
- Generic type parameter tracking
- Interface satisfaction caching

## Integration Points

The symbol resolver integrates with:
1. **Code Generator**: Provides type information during generation
2. **Parser**: Receives AST nodes for analysis
3. **LSP**: Could provide hover information and go-to-definition
4. **Formatter**: Could provide type-aware formatting decisions

## Conclusion

The symbol resolver represents a sophisticated approach to type resolution in a template language, leveraging Go's type system while maintaining the template files as the source of truth. Its overlay-based design enables accurate type checking without requiring generated files to exist on disk, making it suitable for both CLI and IDE scenarios.