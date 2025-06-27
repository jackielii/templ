# Symbol Resolver Architecture

This document provides an in-depth analysis of the symbol resolution system in the templ codebase, focusing on `generator/symbol_resolver.go` and its usage patterns.

## Overview

The symbol resolver is the central component for type resolution in templ, enabling proper type checking and code generation for templ templates. It leverages Go's type system through overlays instead of string-based parsing. The system has been unified into a single resolver that combines component, variable, and expression resolution capabilities.

It should be used in the following way:

1. at the start of the templ generation process in `../cmd/templ/generatecmd/cmd.go`, we want to add a preprocessing step that walks all the files needs generation, symbol resolver should receive these files.
2. parse all the files and find out which ones require type analysis or symbols resolution, for a start, let's use all of them.
4. use `golang.org/x/tools/go/packages` to load the packages and the dependencies of the packages.

### Performance Critical: Package Loading Strategy

**IMPORTANT**: Always load all packages in a single `packages.Load` call rather than loading them one by one. This is a critical performance optimization that reduces processing time from over a minute to approximately 1 second.

```go
// CORRECT: Load all packages at once
loadPaths := []string{dir1, dir2, dir3, ...}
pkgs, err := packages.Load(cfg, loadPaths...)

// WRONG: Loading packages one by one (causes severe performance degradation)
for _, dir := range dirs {
    pkgs, err := packages.Load(cfg, dir) // DON'T DO THIS
}
```

The single-call approach allows the Go packages system to optimize dependency resolution and avoid redundant work.

### Correct LoadMode Configuration

**RECOMMENDED**: Use the predefined `packages.LoadSyntax` constant for optimal performance and correctness:

```go
cfg := &packages.Config{
    Mode:    packages.LoadSyntax,
    Dir:     moduleRoot,
    Overlay: overlays,
}
```

`LoadSyntax` is equivalent to:
```go
LoadSyntax = LoadTypes | NeedSyntax | NeedTypesInfo
// Which expands to:
// LoadTypes = LoadImports | NeedTypes | NeedTypesSizes
// LoadImports = LoadFiles | NeedImports
// LoadFiles = NeedName | NeedFiles | NeedCompiledGoFiles
```

Key points:
- **NeedTypesInfo requires NeedTypes**: Using NeedTypesInfo alone will result in nil TypesInfo (known bug)
- **NeedDeps is NOT required**: Dependencies are automatically loaded as needed for type checking
- **Performance**: Using LoadSyntax without NeedDeps is ~5x faster than LoadAllSyntax

### Package Caching Strategy

Packages must be cached by multiple keys due to how `packages.Load` returns results:
1. **By PkgPath**: The canonical import path (e.g., "github.com/user/project/pkg")
2. **By ID**: May be a relative path (e.g., "./generator/test-element-component")
3. **By Directory**: Absolute filesystem path where the package resides

This multi-key caching is necessary because:
- The same package may be referenced differently depending on context
- Relative imports in patterns result in IDs that differ from PkgPath
- Directory-based lookups are needed for local component resolution

### Module Boundary Limitation and Solution

**CRITICAL LIMITATION**: The overlay feature in `golang.org/x/tools/go/packages` does not work properly across module boundaries. This is a known issue documented in:
- [Issue #71075: overlay does not work properly with external module files](https://github.com/golang/go/issues/71075)
- [Issue #71098: overlay does not cause ExportFile to be cleared](https://github.com/golang/go/issues/71098)

**Symptoms**: When loading packages from multiple modules in a single `packages.Load` call with overlays:
- Packages may have empty scopes despite successful loading
- Type information may be missing or incomplete
- Cross-module imports may fail to resolve

**Solution**: Load packages grouped by module, not just by directory:

```go
// 1. First, identify module boundaries by finding go.mod files
moduleFiles := make(map[string][]string) // module root -> files
for _, file := range templFiles {
    moduleRoot := findModuleRoot(filepath.Dir(file))
    moduleFiles[moduleRoot] = append(moduleFiles[moduleRoot], file)
}

// 2. Load packages for each module separately
for moduleRoot, files := range moduleFiles {
    // Group by package within this module
    packageDirs := make(map[string][]string)
    for _, file := range files {
        dir := filepath.Dir(file)
        packageDirs[dir] = append(packageDirs[dir], file)
    }
    
    // Load all packages in this module together
    loadPackagesForModule(moduleRoot, packageDirs)
}
```

This approach ensures:
- Overlays work correctly within each module
- Type information is properly populated
- Cross-module references can still be resolved through the package cache

**Module Loading Order**: The order in which modules are loaded does not matter. Each `packages.Load` call independently resolves all necessary dependencies within that module's context. Cross-module references are resolved later through the global package cache during symbol resolution.

### Element component

Element component use the HTML element syntax but it accepts any valid Go expression as it's tag name. We want to support the following variants:

1. `<Button>Click me</Button>` - A `func Button(...) templ.Component` function that returns a templ.Component
2. `<mycomponent>Click me</mycomponent>` - A `func mycomponent(...) templ.Component` function that returns a templ.Component
3. `<p.mycomponent>Click me</p.mycomponent>` - A `func (p *mystruct) mycomponent(...) templ.Component` function that returns a templ.Component.
4. `<pkg.Custom>Click me</pkg.Custom>` - A `func Custom(...) templ.Component` function from external package `pkg` that returns a templ.Component, where `pkg` is a package or package alias.
5. `<pkg.Custom>Click me</pkg.Custom>` - A `type Custom ...` type from external package `pkg` that implements templ.Component, where `pkg` is a package or package alias.
6. `<myValue>...</myValue>` - A value of a type that implements the `templ.Component` interface. I.e. has a method `func (s *myType) Render(context.Context, w io.Writer) error`. In addition, if this value is a struct value, we also support populating its fields with the attributes.

For lowercase tags, we can try to resolve it locally first and if it fails, we'll assume it's a plain HTML tag and not a templ component.
