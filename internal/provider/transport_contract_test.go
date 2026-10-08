package provider

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Resources may use Redshift SQL semantics, but must not construct transport clients.
func TestResourcesDoNotImportSQLTransports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, name := range files {
		if name == "provider.go" || name == "connection.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
			require.NoError(t, err)
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				require.NoError(t, err)
				assert.NotContains(t, path, "/internal/redshiftdata")
				assert.NotContains(t, path, "/internal/redshiftconn")
				assert.NotContains(t, path, "aws-sdk-go-v2/service/")
				assert.NotContains(t, path, "github.com/jackc/pgx")
			}
		})
	}
}
