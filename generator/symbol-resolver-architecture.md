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

## Current Architecture Problem

### The Global Instance Challenge

Currently, the symbol resolver is instantiated as a global singleton (`globalSymbolResolver`), but there are fundamental issues with this approach:

1. **Overlay Building**: The overlay (fake `_templ.go` files) needs to be built comprehensively based on the topological order of package dependencies.
   - Leaf packages must be loaded first to avoid circular dependencies
   - The overlay can't be built gradually as each generator instance runs

2. **Resource Efficiency**: With the current design where each generator creates its own instance:
   - Symbol resolution data is recreated for every file being generated
   - Package loading happens repeatedly for the same packages
   - This defeats the purpose of caching and reusing type information

3. **Selective Enablement**: Symbol resolution should only be enabled when ElementComponent syntax is detected in the codebase.

### Ideal Architecture Design

The ideal process follows a dependency-aware approach:

1. **Scanning Phase**:
   - Scan all files to find those using ElementComponent syntax
   - Identify dependencies between packages
   - Build a dependency tree with proper hierarchy

2. **Dependency Tree Structure**:
   - **Root nodes**: Packages that use ElementComponent syntax
   - **Leaf nodes**: Packages that may or may not use ElementComponent syntax
   - The tree represents the dependency flow from packages using ElementComponents down to their dependencies

3. **Topological Processing**:
   - Sort packages topologically based on dependencies
   - Group parsed files into trees based on their ElementComponent usage
   - Process leaf packages first, then work up to root packages

4. **Generation Phase Optimization**:
   - When encountering an ElementComponent during generation
   - Look up the pre-built dependency tree
   - Ensure leaf packages are loaded first
   - Avoid overloading or redundant processing

This approach ensures:
- No redundant package loading
- Proper dependency resolution order
- Efficient memory usage
- Only processes what's necessary

### Key Insight: Internal vs External Packages

The dependency tree naturally divides packages into two categories:

1. **Internal Packages** (in the dependency tree):
   - Packages that use ElementComponent syntax (roots)
   - Their dependencies within the same module/project
   - These need overlays generated from their `.templ` files
   - Must be preloaded in topological order

2. **External Packages** (outside the dependency tree):
   - Standard library packages
   - Third-party dependencies
   - Packages that don't participate in ElementComponent resolution
   - Loaded on-demand without overlays (like regular Go packages)

### Current Workaround: g.enableSymbolResolution

The `g.enableSymbolResolution` flag is a temporary hack because the ideal dependency-aware logic hasn't been implemented yet. This flag currently:
- Manually controls when symbol resolution is active
- Doesn't consider the dependency tree structure
- Leads to inefficient processing

### Goals for the Redesigned Architecture

1. **Smart Dependency Detection**:
   - Build a complete dependency graph of packages
   - Identify which packages use ElementComponent syntax
   - Create a hierarchical tree structure

2. **Efficient Preprocessing**:
   - Process packages in correct topological order
   - Build overlays only for packages that need them
   - Cache results for reuse during generation

3. **Optimized Generation**:
   - Use pre-computed dependency trees
   - Load packages in correct order when needed
   - Eliminate redundant processing

4. **Automatic Enablement**:
   - Remove the need for manual `enableSymbolResolution` flag
   - Automatically detect when symbol resolution is needed
   - Enable only for relevant file trees

### Implementation Plan

1. **Phase 1: ElementComponent Detection**
   - Scan all `.templ` files for `<ComponentName />` syntax
   - Build a map of packages using ElementComponents
   - Mark packages as "roots" or "leaves" in dependency tree

2. **Phase 2: Dependency Graph Construction**
   - Parse imports from all templ files
   - Build directed graph of package dependencies
   - Identify connected components containing ElementComponents

3. **Phase 3: Topological Ordering**
   - Sort packages based on dependencies
   - Group into trees with ElementComponent packages at roots
   - Ensure leaf packages come before their dependents

4. **Phase 4: Preload All Required Packages**
   - Load all packages in the dependency tree with overlays
   - Process in topological order (leaves first)
   - Cache all type information globally
   - This ensures all necessary packages are ready before generation

5. **Phase 5: Generation with Smart Package Resolution**
   - During generation, if a package is requested:
     - If it's in our dependency tree: return cached data (already loaded)
     - If it's outside our dependency tree: treat as external package
     - External packages are loaded without overlays (like go mod externals)
   - This prevents redundant loading and ensures correct behavior

## Conclusion

The unified symbol resolver represents a significant evolution in templ's type resolution capabilities. By combining template caching, expression analysis, and context-aware variable tracking into a single cohesive system, it provides:

- **Better Developer Experience**: Accurate type information throughout templates
- **Enhanced Safety**: Proper variable scoping and type checking
- **Improved Performance**: Strategic caching and lazy loading
- **Cleaner Architecture**: Single resolver instead of multiple systems

The system maintains Go's type safety while providing the flexibility needed for a template language, making it suitable for both CLI generation and IDE integration scenarios.