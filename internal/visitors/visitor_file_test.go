package visitors

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"github.com/scip-code/scip-go/internal/document"
	"github.com/scip-code/scip-go/internal/lookup"
	"github.com/scip-code/scip/bindings/go/scip"
	"golang.org/x/tools/go/packages"
)

func TestGoDocumentDeclaresBytePositionEncoding(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", "package sample\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg := &packages.Package{Fset: fset, TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{}}}
	visitor := NewFileVisitor(&document.Document{RelativePath: "fixture.go"}, pkg, file, lookup.NewPackageSymbols(pkg), nil)
	if got := visitor.ToScipDocument().PositionEncoding; got != scip.PositionEncoding_UTF8CodeUnitOffsetFromLineStart {
		t.Fatalf("SCIP document position encoding = %v; want UTF-8 bytes", got)
	}
}

func TestCollectDeclarationRanges(t *testing.T) {
	source := `package sample
// Widget explains the declaration.
type Widget struct {
	// Field explains both names.
	Field, Other string
	Embedded
}
// Mode explains the value.
var Mode = 1
const Limit = 3
type (
	Alias = Widget
	OtherType struct { Value int }
)
func (w *Widget) Do(arg int) { local := 1; _ = local }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	pkg := &packages.Package{Fset: fset, TypesInfo: &types.Info{Defs: map[*ast.Ident]types.Object{}}}
	ranges := collectDeclarationRanges(pkg, file)
	declarations := make(map[string]ast.Node)
	ast.Inspect(file, func(n ast.Node) bool {
		switch item := n.(type) {
		case *ast.TypeSpec:
			declarations[item.Name.Name] = item
		case *ast.ValueSpec:
			for _, name := range item.Names {
				declarations[name.Name] = item
			}
		case *ast.Field:
			for _, name := range item.Names {
				declarations[name.Name] = item
			}
		}
		return true
	})
	for _, name := range []string{"Widget", "Field", "Other", "Embedded", "Mode", "Limit", "Alias", "OtherType", "Value"} {
		var ident *ast.Ident
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == name && ident == nil {
				ident = id
			}
			return true
		})
		if ident == nil || ranges[ident.Pos()] == nil {
			t.Fatalf("missing enclosing range for %s", name)
		}
		decl := declarations[name]
		if name == "Widget" || name == "Mode" || name == "Limit" {
			// A single-spec declaration includes its `type`/`var`/`const` token.
			for _, entry := range file.Decls {
				if gen, ok := entry.(*ast.GenDecl); ok && len(gen.Specs) == 1 && gen.Specs[0] == decl {
					decl = gen
				}
			}
		}
		if decl == nil {
			// The unnamed embedded field is represented by its type identifier.
			if name != "Embedded" {
				t.Fatalf("missing AST declaration for %s", name)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				if field, ok := n.(*ast.Field); ok && field.Type == ident {
					decl = field
				}
				return true
			})
		}
		if decl == nil {
			t.Fatalf("missing enclosing AST node for %s", name)
		}
		start := decl.Pos()
		if field, ok := decl.(*ast.Field); ok && field.Doc != nil {
			start = field.Doc.Pos()
		}
		if name == "Widget" || name == "Mode" {
			for _, entry := range file.Decls {
				if gen, ok := entry.(*ast.GenDecl); ok && gen == decl && gen.Doc != nil {
					start = gen.Doc.Pos()
				}
			}
		}
		expected := scipRange(fset.PositionFor(start, false), fset.PositionFor(decl.End(), false), nil)
		if !reflect.DeepEqual(*ranges[ident.Pos()], expected) {
			t.Errorf("%s enclosing range = %v; want %v", name, ranges[ident.Pos()], expected)
		}
	}
	for _, name := range []string{"arg", "local"} {
		ast.Inspect(file, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == name && ranges[ident.Pos()] != nil {
				t.Errorf("local %s unexpectedly has a global declaration range", name)
			}
			return true
		})
	}
}
