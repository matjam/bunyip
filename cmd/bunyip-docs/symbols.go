package main

import (
	"fmt"
	"go/ast"
	"go/doc"
	"html"
	"strings"
)

// packageSymbols includes declarations go/doc associates with a type as
// well as package-level declarations. Every URL is emitted only once.
func packageSymbols(p *Package) []symbol {
	var out []symbol
	seen := map[string]bool{}
	add := func(name, kind string) {
		if seen[name] {
			return
		}
		seen[name] = true
		out = append(out, symbol{Name: name, Pkg: shortName(p.Rel), URL: p.URL + "#" + name, Kind: kind})
	}
	values := func(list []*Value, kind string) {
		for _, v := range list {
			for _, name := range v.Names {
				add(name, kind)
			}
		}
	}
	values(p.Consts, "const")
	values(p.Vars, "var")
	for _, f := range p.Funcs {
		add(f.ID, "func")
	}
	for _, t := range p.Types {
		add(t.Name, "type")
		values(t.Consts, "const")
		values(t.Vars, "var")
		for _, f := range t.Funcs {
			add(f.ID, "func")
		}
		for _, m := range t.Methods {
			add(m.ID, "method")
		}
		for _, m := range t.Members {
			add(m.Name, m.Kind)
		}
	}
	return out
}

// typeMembers indexes the exported members visible in the declaration.
// Promoted members need type checking and are represented by their embedded
// type instead; explicitly declared interface methods are included here.
func typeMembers(t *doc.Type) []symbol {
	var out []symbol
	for _, spec := range t.Decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Name.Name != t.Name {
			continue
		}
		var fields *ast.FieldList
		kind := "field"
		switch typ := ts.Type.(type) {
		case *ast.StructType:
			fields = typ.Fields
		case *ast.InterfaceType:
			fields, kind = typ.Methods, "method"
		}
		if fields == nil {
			continue
		}
		for _, field := range fields.List {
			names := field.Names
			if len(names) == 0 && kind == "field" {
				if name := embeddedName(field.Type); name != nil {
					names = []*ast.Ident{name}
				}
			}
			for _, name := range names {
				if name.IsExported() {
					out = append(out, symbol{Name: t.Name + "." + name.Name, Kind: kind})
				}
			}
		}
	}
	return out
}

func embeddedName(expr ast.Expr) *ast.Ident {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr
	case *ast.SelectorExpr:
		return expr.Sel
	case *ast.StarExpr:
		return embeddedName(expr.X)
	case *ast.IndexExpr:
		return embeddedName(expr.X)
	case *ast.IndexListExpr:
		return embeddedName(expr.X)
	}
	return nil
}

func writeAnchors(b *strings.Builder, names []string) {
	for _, name := range names {
		fmt.Fprintf(b, "<a id=\"%s\"></a>\n\n", html.EscapeString(name))
	}
}
