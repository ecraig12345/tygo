package tygo

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/packages"
)

// syntaxFilePath resolves the source path represented by a syntax tree.
func syntaxFilePath(pkg *packages.Package, file *ast.File) string {
	if pkg == nil || pkg.Fset == nil {
		return ""
	}
	return pkg.Fset.PositionFor(file.Pos(), false).Filename
}

// preProcessEnums scans the file for const declarations that will be converted to enums
// and marks the corresponding types to prevent duplicate type declarations
func (g *PackageGenerator) preProcessEnums(file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		if decl, ok := n.(*ast.GenDecl); ok && decl.Tok == token.CONST {
			if enumGroup := g.detectEnumGroup(decl); enumGroup != nil {
				g.generatedEnums[enumGroup.typeName] = true
			}
		}
		return true
	})
}

// generateFile writes the generated code for a single file to the given strings.Builder.
func (g *PackageGenerator) generateFile(s *strings.Builder, file *ast.File, filepath string) {
	// First pass: identify types that will be generated as enums
	g.preProcessEnums(file)

	first := true

	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		// GenDecl can be an import, type, var, or const expression
		case *ast.GenDecl:
			if x.Tok == token.IMPORT {
				return false
			}
			isEmit := false
			if x.Tok == token.VAR {
				isEmit = g.isEmitVar(x)
				if !isEmit {
					return false
				}
			}

			if first {
				if filepath != "" {
					g.writeFileSourceHeader(s, filepath, file)
				}
				first = false
			}
			if isEmit {
				g.emitVar(s, x)
				return false
			}
			g.writeGroupDecl(s, x)
			return false
		}
		return true
	})
}

func (g *PackageGenerator) Generate() (string, error) {
	// Resolve unions before writing so later declarations can be included.
	if err := g.analyzeInterfaceUnions(); err != nil {
		return "", err
	}

	s := new(strings.Builder)

	g.writeFileCodegenHeader(s)
	g.writeFileFrontmatter(s)

	for _, file := range g.pkg.Syntax {
		filepath := syntaxFilePath(g.pkg, file)
		if filepath == "" {
			return "", fmt.Errorf("failed to resolve source path for package %s", g.pkg.PkgPath)
		}
		if g.conf.IsFileIgnored(filepath) {
			continue
		}

		g.generateFile(s, file, filepath)
	}

	return s.String(), nil
}
