# Unified Symbol Resolution Proposal for templ

## Current State

The templ codebase currently has two separate symbol resolvers:

1. **TemplSignatureResolver** (`templsignatureresolver.go`)
   - Extracts signatures from templ templates in the current file
   - Uses Go's AST parser to parse template declarations
   - Only works with local templates in the same file

2. **SymbolResolver** (`symbolresolver.go`)
   - Resolves Go components across packages using `golang.org/x/tools/go/packages`
   - Can resolve functions and types that implement `templ.Component`
   - Cannot see templ templates from other packages

## The Problem

When resolving element components like `<Button />` or `<pkg.Card />`, we need to resolve both:
- templ templates (e.g., `templ Button(label string)`)
- Go types implementing `templ.Component` (e.g., `type NavBar struct{}`)

Currently, cross-package templ template resolution doesn't work because `packages.Load` only sees Go files, not `.templ` files.

## Proposed Solution: Overlay-Based Unified Resolution

### Core Concept

Use `packages.Load`'s overlay feature to make templ templates visible as Go functions:

1. When resolving a component from another package:
   - Find all `.templ` files in that package
   - Parse them to extract template signatures
   - Generate Go stub code that represents these templates
   - Use `packages.Load` with an overlay containing the generated stubs

2. This allows us to resolve both templ templates and Go components through the same mechanism

### Implementation Approach

```go
type UnifiedResolver struct {
    workingDir string
    cache      map[string]ComponentSignature
    overlay    map[string][]byte
}

func (r *UnifiedResolver) ResolveComponent(pkgPath, componentName string) (ComponentSignature, error) {
    // Step 1: Load package metadata to find files
    cfg := &packages.Config{
        Mode: packages.NeedName | packages.NeedFiles,
        Dir:  r.workingDir,
    }
    
    pkgs, _ := packages.Load(cfg, pkgPath)
    
    // Step 2: Find and parse .templ files
    for _, goFile := range pkg.GoFiles {
        dir := filepath.Dir(goFile)
        templFiles, _ := filepath.Glob(filepath.Join(dir, "*.templ"))
        
        for _, templFile := range templFiles {
            content, _ := os.ReadFile(templFile)
            tf, _ := parser.ParseString(string(content))
            
            // Step 3: Generate Go stub overlay
            overlayPath := strings.TrimSuffix(templFile, ".templ") + "_templ.go"
            r.overlay[overlayPath] = generateStub(tf)
        }
    }
    
    // Step 4: Load package with overlay
    cfg2 := &packages.Config{
        Mode:    packages.NeedTypes,
        Overlay: r.overlay,
    }
    
    pkgs2, _ := packages.Load(cfg2, pkgPath)
    
    // Step 5: Resolve component through unified type system
    obj := pkgs2[0].Types.Scope().Lookup(componentName)
    return extractSignature(obj), nil
}
```

### Stub Generation Example

For a templ file containing:
```templ
templ Button(label string) {
    <button>{ label }</button>
}
```

Generate overlay stub:
```go
package mypackage

import "github.com/a-h/templ"

func Button(label string) templ.Component {
    return nil // stub
}
```

### Benefits

1. **Unified Resolution**: Both templ templates and Go components resolved through the same mechanism
2. **Type Safety**: Full type information available through Go's type system
3. **Cross-Package Support**: Can resolve components from any package
4. **Caching**: Overlay and results can be cached for performance
5. **Compatibility**: Works with existing `packages.Load` infrastructure

### Integration Points

1. **Element Component Resolution** (`generator_element_component.go`):
   - Use unified resolver in `collectAndResolveComponents()`
   - Replace separate `SymbolResolver` and `TemplSignatureResolver` calls

2. **LSP Integration**:
   - The same approach can be used for code completion and hover information
   - Overlay can be shared across LSP operations

### Performance Considerations

1. **Caching**: 
   - Cache parsed templ files
   - Cache generated overlays
   - Cache resolution results

2. **Lazy Loading**:
   - Only generate overlays for packages actually referenced
   - Only parse templ files when needed

3. **Incremental Updates**:
   - Track file changes to invalidate specific cache entries
   - Reuse overlays when files haven't changed

### Challenges and Solutions

1. **Import Resolution**:
   - Challenge: templ files may have imports that need to be included in stubs
   - Solution: Parse and include imports from templ files in generated stubs

2. **Generic Components**:
   - Challenge: templ supports generic syntax that needs proper stub generation
   - Solution: Generate generic function stubs matching the templ syntax

3. **Performance at Scale**:
   - Challenge: Large projects with many packages
   - Solution: Aggressive caching and lazy loading strategies

## Next Steps

1. Prototype the unified resolver as a separate module
2. Test with real-world templ projects for edge cases
3. Integrate into element component resolution
4. Extend to LSP for better IDE support
5. Add comprehensive caching layer

## Alternative Approaches Considered

1. **Separate templ Package Metadata**:
   - Maintain separate metadata files for templ components
   - Rejected: Adds complexity and synchronization issues

2. **Runtime Reflection**:
   - Use runtime reflection to discover components
   - Rejected: Loses compile-time type safety

3. **Code Generation Enhancement**:
   - Generate additional metadata during templ generation
   - Still viable: Could complement overlay approach

## Conclusion

The overlay-based unified resolution approach leverages Go's existing tooling (`golang.org/x/tools/go/packages`) to provide a robust solution for cross-package component resolution. It maintains type safety while adding support for templ templates alongside Go components.