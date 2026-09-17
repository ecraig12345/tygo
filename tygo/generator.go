package tygo

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/packages"
)

// Generator for one or more input packages, responsible for linking
// them together if necessary.
type Tygo struct {
	conf *Config

	packageGenerators map[string]*PackageGenerator
}

// Responsible for generating the code for an input package
type PackageGenerator struct {
	conf            *PackageConfig
	pkg             *packages.Package
	generatedEnums  map[string]bool // Track types that have been generated as enums
	interfaceUnions map[*ast.TypeSpec][]string
}

func New(config *Config) *Tygo {
	return &Tygo{
		conf:              config,
		packageGenerators: make(map[string]*PackageGenerator),
	}
}

func (g *Tygo) SetTypeMapping(goType string, tsType string) {
	for _, p := range g.conf.Packages {
		p.TypeMappings[goType] = tsType
	}
}

// packageHasUnionDirective scans included files without requiring type information.
func packageHasUnionDirective(pkg *packages.Package, config *PackageConfig) bool {
	for _, file := range pkg.Syntax {
		if config.IsFileIgnored(syntaxFilePath(pkg, file)) {
			continue
		}
		if fileHasUnionDirective(file) {
			return true
		}
	}
	return false
}

func (g *Tygo) Generate() error {
	// Load syntax first so packages without union directives avoid type checking.
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedFiles,
	}, g.conf.PackageNames()...)
	if err != nil {
		return err
	}

	packageConfigs := make([]*PackageConfig, len(pkgs))
	needsTypes := make([]bool, len(pkgs))
	typedPackagePaths := make([]string, 0)
	for i, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return fmt.Errorf("%+v", pkg.Errors)
		}

		if len(pkg.GoFiles) == 0 {
			return fmt.Errorf("no input go files for package index %d", i)
		}

		pkgConfig := g.conf.PackageConfig(pkg.ID)
		packageConfigs[i] = pkgConfig
		needsTypes[i] = packageHasUnionDirective(pkg, pkgConfig)
		if needsTypes[i] {
			typedPackagePaths = append(typedPackagePaths, pkg.PkgPath)
		}
	}

	// Reload only annotated packages with the type data needed for method-set checks.
	typedPackagesByPath := make(map[string]*packages.Package, len(typedPackagePaths))
	if len(typedPackagePaths) > 0 {
		typedPackages, err := packages.Load(&packages.Config{
			Mode: packages.NeedName | packages.NeedSyntax | packages.NeedFiles | packages.NeedTypes | packages.NeedTypesInfo,
		}, typedPackagePaths...)
		if err != nil {
			return err
		}
		for _, pkg := range typedPackages {
			if len(pkg.Errors) > 0 {
				return fmt.Errorf("%+v", pkg.Errors)
			}
			typedPackagesByPath[pkg.PkgPath] = pkg
		}
	}

	for i, pkg := range pkgs {
		pkgConfig := packageConfigs[i]
		if needsTypes[i] {
			typedPkg, ok := typedPackagesByPath[pkg.PkgPath]
			if !ok {
				return fmt.Errorf("failed to load type information for package %s", pkg.PkgPath)
			}
			pkg = typedPkg
		}

		pkgGen := &PackageGenerator{
			conf:           pkgConfig,
			pkg:            pkg,
			generatedEnums: make(map[string]bool),
		}
		g.packageGenerators[pkg.PkgPath] = pkgGen
		code, err := pkgGen.Generate()
		if err != nil {
			return err
		}

		outPath := pkgGen.conf.ResolvedOutputPath(filepath.Dir(pkg.GoFiles[0]))
		err = os.MkdirAll(filepath.Dir(outPath), os.ModePerm)
		if err != nil {
			return nil
		}

		err = os.WriteFile(outPath, []byte(code), 0o664)
		if err != nil {
			return nil
		}
	}
	return nil
}
