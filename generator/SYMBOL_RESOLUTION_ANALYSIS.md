# Symbol Resolution Analysis for templ

## Summary

After analyzing the templ codebase and experimenting with `golang.org/x/tools/go/packages`, I've identified how we can improve symbol resolution for element components.

## Current Architecture

### Two Separate Resolvers

1. **TemplSignatureResolver** (`templsignatureresolver.go`):
   - Parses templ templates in the current file using Go's AST parser
   - Extracts function signatures from `templ Name(params)` declarations
   - Only works with templates in the same file
   - Cannot resolve cross-package templates

2. **SymbolResolver** (`symbolresolver.go`):
   - Uses `golang.org/x/tools/go/packages` to resolve Go symbols across packages
   - Can find functions and types that implement `templ.Component`
   - Works with generated `_templ.go` files but often encounters compilation errors
   - Cannot see original `.templ` template definitions from other packages

## The Core Problem

When you write `<pkg.Button />` in a templ file:
- If `Button` is a templ template in another package, it can't be resolved
- The `packages.Load` API only sees Go files, not `.templ` source files
- Generated `_templ.go` files may have compilation errors that prevent resolution

## Proposed Solution: Overlay-Based Resolution

### Key Insight

`packages.Load` supports an "overlay" feature that allows injecting virtual files into the package loading process. We can use this to make templ templates visible as Go functions.

### How It Works

1. **Discovery Phase**:
   - Use `packages.Load` to find the target package's file locations
   - Scan for `.templ` files in the package directory
   
2. **Parsing Phase**:
   - Parse each `.templ` file to extract template signatures
   - Use the existing templ parser infrastructure
   
3. **Overlay Generation**:
   - Generate minimal Go stub code for each template
   - Example: `templ Button(text string)` becomes:
     ```go
     func Button(text string) templ.Component { return nil }
     ```
   
4. **Resolution Phase**:
   - Use `packages.Load` with the overlay to get type information
   - Both templ templates and Go components are now visible
   - Extract parameter types and signatures

### Benefits

1. **Unified Resolution**: Single mechanism for both templ templates and Go components
2. **Type Safety**: Full Go type checking on template parameters
3. **Cross-Package**: Works across package boundaries
4. **No File Generation**: Uses in-memory overlays, no disk I/O
5. **Error Resilient**: Doesn't depend on generated files compiling

### Implementation Sketch

```go
type UnifiedResolver struct {
    workingDir string
    cache      map[string]ComponentSignature
}

func (r *UnifiedResolver) ResolveComponent(pkgPath, name string) (ComponentSignature, error) {
    // Step 1: Find package files
    cfg := &packages.Config{Mode: packages.NeedFiles}
    pkgs, _ := packages.Load(cfg, pkgPath)
    
    // Step 2: Find and parse .templ files
    overlay := make(map[string][]byte)
    for _, goFile := range pkgs[0].GoFiles {
        dir := filepath.Dir(goFile)
        templFiles, _ := filepath.Glob(filepath.Join(dir, "*.templ"))
        
        for _, templFile := range templFiles {
            content, _ := os.ReadFile(templFile)
            tf, _ := parser.ParseString(string(content))
            
            // Generate stub Go code
            stubPath := templFile + ".overlay.go"
            overlay[stubPath] = generateStub(tf)
        }
    }
    
    // Step 3: Load with overlay for type resolution
    cfg2 := &packages.Config{
        Mode:    packages.NeedTypes,
        Overlay: overlay,
    }
    pkgs2, _ := packages.Load(cfg2, pkgPath)
    
    // Step 4: Look up and return component signature
    obj := pkgs2[0].Types.Scope().Lookup(name)
    return extractSignature(obj), nil
}
```

## Integration Points

### 1. Element Component Resolution
- File: `generator_element_component.go`
- Function: `collectAndResolveComponents()`
- Replace dual resolver approach with unified resolver

### 2. Import Resolution
- Currently handled separately for templ imports vs Go imports
- Can be unified through the same overlay mechanism

### 3. LSP Integration
- The overlay approach can provide better IDE support
- Real-time updates as files change

## Performance Considerations

1. **Caching**:
   - Cache parsed templ files
   - Cache generated overlays
   - Cache resolution results
   - Invalidate on file changes

2. **Lazy Loading**:
   - Only process packages that are actually imported
   - Only parse files when components are referenced

3. **Batch Processing**:
   - Resolve multiple components in one pass
   - Share overlay generation across operations

## Next Steps

1. **Prototype**: Build a working unified resolver as a separate module
2. **Test**: Validate with real templ projects
3. **Integrate**: Replace existing resolvers in element component generation
4. **Optimize**: Add caching and performance improvements
5. **Extend**: Use for LSP and other tooling

## Conclusion

Using `packages.Load` with overlays provides an elegant solution to unify templ template and Go component resolution. This approach leverages Go's existing type system while adding support for templ's template syntax, enabling robust cross-package component resolution without depending on generated files.