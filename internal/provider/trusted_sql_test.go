package provider

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sqlclientImportPath identifies the package whose trusted-text types are guarded, whatever a file names it.
const sqlclientImportPath = "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// trustedTextTypes are the sqlclient types that builders emit without quoting.
var trustedTextTypes = map[string]bool{"Keyword": true, "UserSQL": true}

// trustedAnnotation marks a reviewed conversion of runtime text on the same or the previous line.
const trustedAnnotation = "//sql:trusted"

// packageConstants collects the names declared with const anywhere in the files, including inside functions.
// Shadowing is ignored; a variable reusing a constant's name would be a review smell of its own.
func packageConstants(files []*ast.File) map[string]bool {
	constants := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if declaration, ok := node.(*ast.GenDecl); ok && declaration.Tok == token.CONST {
				for _, spec := range declaration.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						constants[name.Name] = true
					}
				}
			}
			return true
		})
	}
	return constants
}

// untrustedConversions reports conversions of non-constant expressions to a trusted-text type that lack the
// annotation. Without type information it accepts string literals, declared constants, their concatenation and
// nested trusted conversions of those, which covers every form the compiler would also accept implicitly.
func untrustedConversions(fileSet *token.FileSet, file *ast.File, constants map[string]bool) []string {
	var aliases []string
	for _, spec := range file.Imports {
		if path, _ := strconv.Unquote(spec.Path.Value); path == sqlclientImportPath {
			alias := filepath.Base(path)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			aliases = append(aliases, alias)
		}
	}
	annotated := map[int]bool{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, trustedAnnotation) {
				annotated[fileSet.Position(comment.Slash).Line] = true
			}
		}
	}
	trustedConversion := func(call *ast.CallExpr) bool {
		if len(call.Args) != 1 {
			return false
		}
		switch function := ast.Unparen(call.Fun).(type) {
		case *ast.SelectorExpr:
			qualifier, ok := function.X.(*ast.Ident)
			return ok && trustedTextTypes[function.Sel.Name] && slices.Contains(aliases, qualifier.Name)
		case *ast.Ident:
			// A dot import makes the bare type name a conversion.
			return trustedTextTypes[function.Name] && slices.Contains(aliases, ".")
		}
		return false
	}
	var constant func(ast.Expr) bool
	constant = func(expression ast.Expr) bool {
		switch expression := ast.Unparen(expression).(type) {
		case *ast.BasicLit:
			return expression.Kind == token.STRING
		case *ast.Ident:
			return constants[expression.Name]
		case *ast.BinaryExpr:
			return expression.Op == token.ADD && constant(expression.X) && constant(expression.Y)
		case *ast.CallExpr:
			return trustedConversion(expression) && constant(expression.Args[0])
		}
		return false
	}
	var findings []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !trustedConversion(call) || constant(call.Args[0]) {
			return true
		}
		position := fileSet.Position(call.Pos())
		if !annotated[position.Line] && !annotated[position.Line-1] {
			findings = append(findings, fmt.Sprintf("%s:%d: %s", filepath.Base(position.Filename), position.Line, types.ExprString(call)))
		}
		return true
	})
	return findings
}

// TestNoUntrustedKeywordConversions keeps runtime text out of unquoted SQL: a Keyword or UserSQL built from a
// variable must come from sqlclient.OneOf, TypeName or CheckUserSQL, or carry a reviewed //sql:trusted annotation.
func TestNoUntrustedKeywordConversions(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	fileSet := token.NewFileSet()
	var files []*ast.File
	for _, path := range paths {
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		require.NoError(t, err)
		files = append(files, file)
	}
	constants := packageConstants(files)
	for _, file := range files {
		assert.Empty(t, untrustedConversions(fileSet, file, constants), "use sqlclient.OneOf, TypeName or CheckUserSQL, or annotate a reviewed conversion with %s", trustedAnnotation)
	}
}

// TestUntrustedConversionDetection checks the detector against the forms it must accept and reject.
func TestUntrustedConversionDetection(t *testing.T) {
	for _, test := range []struct {
		name, body string
		findings   []string
	}{
		{"literal", `_ = sqlclient.Keyword("TABLE")`, nil},
		{"constant", `_ = sqlclient.Keyword(objectTable)`, nil},
		{"local constant", "const local = \"VIEW\"\n_ = sqlclient.Keyword(local)", nil},
		{"constant concatenation", `_ = sqlclient.Keyword(objectTable + " " + "IN SCHEMA")`, nil},
		{"nested constant conversion", `_ = sqlclient.Keyword(sqlclient.Keyword(objectTable))`, nil},
		{"other package", `_ = other.Keyword(value)`, nil},
		{"other type", `_ = sqlclient.Statement(value)`, nil},
		{"variable", `_ = sqlclient.Keyword(value)`, []string{"source.go:9: sqlclient.Keyword(value)"}},
		{"user SQL", `_ = sqlclient.UserSQL(value)`, []string{"source.go:9: sqlclient.UserSQL(value)"}},
		{"call result", `_ = sqlclient.Keyword(fmt.Sprintf("%s", value))`, []string{`source.go:9: sqlclient.Keyword(fmt.Sprintf("%s", value))`}},
		{"concatenated variable", `_ = sqlclient.Keyword("ON " + value)`, []string{`source.go:9: sqlclient.Keyword("ON " + value)`}},
		{"parenthesized type", `_ = (sqlclient.Keyword)(value)`, []string{"source.go:9: (sqlclient.Keyword)(value)"}},
		{"nested variable conversion", `_ = sqlclient.Keyword(sqlclient.Keyword(value))`, []string{"source.go:9: sqlclient.Keyword(sqlclient.Keyword(value))", "source.go:9: sqlclient.Keyword(value)"}},
		{"same-line annotation", `_ = sqlclient.Keyword(value) //sql:trusted checked against the catalog`, nil},
		{"previous-line annotation", "//sql:trusted checked against the catalog\n_ = sqlclient.Keyword(value)", nil},
		{"distant annotation", "//sql:trusted\n\n_ = sqlclient.Keyword(value)", []string{"source.go:11: sqlclient.Keyword(value)"}},
		{"annotation needs its marker first", `_ = sqlclient.Keyword(value) // see sql:trusted`, []string{"source.go:9: sqlclient.Keyword(value)"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "package provider\n\nimport \"" + sqlclientImportPath + "\"\n\nconst objectTable = \"TABLE\"\n\nfunc f(value string) {\n\t_ = value\n\t" + test.body + "\n}\n"
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, "source.go", source, parser.ParseComments|parser.SkipObjectResolution)
			require.NoError(t, err)
			assert.Equal(t, test.findings, untrustedConversions(fileSet, file, packageConstants([]*ast.File{file})))
		})
	}

	aliased := "package provider\n\nimport dataapi \"" + sqlclientImportPath + "\"\n\nfunc f(value string) { _ = dataapi.Keyword(value); _ = sqlclient.Keyword(value) }\n"
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "aliased.go", aliased, parser.ParseComments|parser.SkipObjectResolution)
	require.NoError(t, err)
	assert.Equal(t, []string{"aliased.go:5: dataapi.Keyword(value)"}, untrustedConversions(fileSet, file, nil), "the import alias, not the package name, identifies sqlclient")

	dotted := "package provider\n\nimport . \"" + sqlclientImportPath + "\"\n\nfunc f(value string) { _ = Keyword(value); _ = Keyword(\"TABLE\") }\n"
	file, err = parser.ParseFile(fileSet, "dotted.go", dotted, parser.ParseComments|parser.SkipObjectResolution)
	require.NoError(t, err)
	assert.Equal(t, []string{"dotted.go:5: Keyword(value)"}, untrustedConversions(fileSet, file, nil))
}
