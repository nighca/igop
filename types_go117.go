//go:build !go1.18
// +build !go1.18

package igop

import (
	"go/ast"
	"go/types"
)

const (
	enabledTypeParam = false
)

func hasTypeParam(t types.Type) bool {
	return false
}

func extractNamed(named *types.Named) (pkgpath string, name string) {
	obj := named.Obj()
	if pkg := obj.Pkg(); pkg != nil {
		pkgpath = pkg.Path()
	}
	name = obj.Name()
	return
}

func (sp *sourcePackage) Load() (err error) {
	if sp.Info == nil {
		sp.Info = &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Implicits:  make(map[ast.Node]types.Object),
			Scopes:     make(map[ast.Node]*types.Scope),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
		}
		if err := types.NewChecker(sp.Context.conf, sp.Context.FileSet, sp.Package, sp.Info).Files(sp.Files); err != nil {
			return err
		}
	}
	return
}