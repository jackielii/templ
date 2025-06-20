# Unified Symbol Resolver - Final Design

## Overview

This document presents the final design for a unified symbol resolver that:
- Uses `modcheck.WalkUp` to automatically detect module roots
- Works across package and module boundaries
- Handles both templ templates and Go components
- Uses overlay-based resolution for type safety

## Key Improvements

### 1. Automatic Module Detection

Instead of requiring a working directory parameter, the resolver uses `modcheck.WalkUp`:

```go
// Before: Required working directory
resolver := NewSymbolResolver(workingDir)

// After: Automatic detection
resolver := NewAutoDetectUnifiedResolver()
sig, err := resolver.ResolveComponentFrom(currentFileDir, pkgPath, componentName)
```

### 2. Context-Aware Resolution

The resolver can start from any directory and find the correct module root:

```go
// Resolving from different contexts
resolver.ResolveComponentFrom("./some/subdir", "pkg/path", "Component")
resolver.ResolveComponentFrom("./another/module", "pkg/path", "Component")
```

## Architecture

### Core Interface

```go
type UnifiedResolver interface {
    // Resolve from a specific directory (for code generation)
    ResolveComponentFrom(fromDir, pkgPath, componentName string) (ComponentSignature, error)
    
    // Resolve from current working directory (for compatibility)
    ResolveComponent(pkgPath, componentName string) (ComponentSignature, error)
}
```

### Implementation Flow

1. **Module Detection**: Use `modcheck.WalkUp(fromDir)` to find module root
2. **Package Loading**: Use `packages.Load` with detected module root
3. **Templ File Discovery**: Find `.templ` files in package directories
4. **Overlay Generation**: Create Go stubs for templ templates
5. **Type Resolution**: Use `packages.Load` with overlay for type information
6. **Signature Extraction**: Extract parameter types and component info

### Example Usage in Generator

```go
// In generator.go - element component resolution
func (g *generator) resolveElementComponent(pkg, name string) (ComponentSignature, error) {
    // Get current file directory
    currentDir := filepath.Dir(g.options.FileName)
    
    // Resolve component
    return g.unifiedResolver.ResolveComponentFrom(currentDir, pkg, name)
}
```

## Test Results

### Module Detection
✅ From subdirectory: Finds parent module root  
✅ From module root: Uses same directory  
✅ From nested module: Finds correct nested root  

### Cross-Module Resolution
✅ Same module: `Button` from main module  
✅ Subpackage: `mod.Text` from subpackage  
✅ External module: `extern.Text` from separate module  
✅ Complex parameters: Multi-parameter components  

### Performance
✅ Caching: Second resolution uses cache  
✅ Overlay reuse: Shared across operations  

## Integration Points

### 1. Replace Existing Resolvers

Current dual approach:
```go
// generator.go
templResolver := make(TemplSignatureResolver)
symbolResolver := SymbolResolver{...}
```

New unified approach:
```go
// generator.go
unifiedResolver := NewAutoDetectUnifiedResolver()
```

### 2. Element Component Generation

In `generator_element_component.go`:
```go
func (g *generator) collectAndResolveComponents() error {
    // Find all element components
    for _, ec := range elementComponents {
        sig, err := g.unifiedResolver.ResolveComponentFrom(
            g.currentFileDir(), 
            ec.PackagePath, 
            ec.ComponentName,
        )
        // ... handle signature
    }
}
```

### 3. WithWorkingDir Option

Update the existing option to use the new resolver:
```go
func WithWorkingDir(dir string) GenerateOpt {
    return func(g *generator) error {
        g.unifiedResolver = NewAutoDetectUnifiedResolver()
        // dir is now auto-detected, but we can still override if needed
        return nil
    }
}
```

## Benefits

### 1. Simplified API
- No need to manually specify working directories
- Consistent with templ's existing patterns
- Self-contained resolver

### 2. Improved Accuracy
- Always finds correct module root
- Handles complex module structures
- Works from any starting location

### 3. Better Error Handling
- Clear error messages with file paths
- Module detection failures are explicit
- Type resolution errors include context

### 4. Performance
- Caching at resolver level
- Overlay reuse across operations
- Lazy loading of packages

## Implementation Plan

### Phase 1: Core Resolver
1. Implement `AutoDetectUnifiedResolver`
2. Add overlay generation logic
3. Implement caching layer

### Phase 2: Integration
1. Update `generator.go` to use unified resolver
2. Replace calls in `generator_element_component.go`
3. Remove `WithWorkingDir` option

### Phase 3: Testing & Optimization
1. Comprehensive test suite
2. Performance optimization
3. Error handling improvements

### Phase 4: Cleanup
1. Remove old `SymbolResolver` and `TemplSignatureResolver`
2. Update documentation
3. Add examples

## Migration Guide

### For Internal Code

Replace:
```go
sr := NewSymbolResolver(workingDir)
sig, err := sr.ResolveComponent(pkgPath, name)
```

With:
```go
resolver := NewAutoDetectUnifiedResolver()
sig, err := resolver.ResolveComponentFrom(currentDir, pkgPath, name)
```

### For External Users

The change is mostly internal, but users benefit from:
- More reliable cross-package component resolution
- Better error messages
- Consistent behavior across different project structures

## Conclusion

The auto-detecting unified resolver provides a robust solution for cross-package component resolution that:
- Eliminates manual working directory management
- Works reliably across complex module structures
- Maintains type safety through overlay-based resolution
- Integrates cleanly with existing templ architecture

This design addresses all the limitations of the current dual-resolver approach while maintaining backward compatibility and improving the developer experience.
