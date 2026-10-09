package sqlclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSQLQuoting checks escaping of embedded identifier and string delimiters.
func TestSQLQuoting(t *testing.T) {
	assert.Equal(t, `"a""b"`, Identifier(`a"b`))
	assert.Equal(t, "'a''b'", Literal("a'b"))
	assert.Equal(t, `'a\\b'`, Literal(`a\b`))
	assert.Equal(t, `'a\\''; DROP USER x; --'`, Literal(`a\'; DROP USER x; --`))
	assert.Equal(t, `'\\\\'''`, Literal(`\\'`))
}
