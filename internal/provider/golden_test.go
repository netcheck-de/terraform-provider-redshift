package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden rewrites golden SQL files; reviewing the resulting diff is the point of the golden tests.
var updateGolden = flag.Bool("update", false, "rewrite golden SQL files under testdata/sql")

// goldenName restricts groups and cases to portable, review-friendly file names.
var goldenName = regexp.MustCompile(`^[a-z0-9_]+$`)

// ddlVerbs are the leading keywords SerializeMutations and the AWS reference expect for catalog writes.
var ddlVerbs = []string{"ALTER", "COMMENT", "CREATE", "DROP", "GRANT", "REVOKE"}

// readVerbs are the leading keywords SerializeMutations lets run concurrently.
var readVerbs = []string{"SELECT", "SHOW"}

// sqlCase names one renderer output within a golden group.
type sqlCase struct {
	// name becomes testdata/sql/<group>/<name>.sql.
	name string
	// render is a func() string, func() []string, func() (string, error), or func() ([]string, error).
	render any
}

// sqlEntry is one recorded client call, including reads, so transcripts pin the complete SQL conversation.
type sqlEntry struct {
	// database is the connection the statement executed in.
	database string
	// sql is the exact statement text sent to the client.
	sql string
	// params are the named bindings sent with the statement.
	params map[string]string
	// err is the client result, recorded so expected failures stay visible.
	err error
}

// recordingClient wraps a fake catalog and records every call in order.
type recordingClient struct {
	// client answers the recorded calls.
	client sqlclient.Client
	// mu keeps entries consistent if a resource ever issues concurrent reads.
	mu sync.Mutex
	// entries holds the calls since the last take.
	entries []sqlEntry
}

// Query forwards to the wrapped client and records the call with its result.
func (c *recordingClient) Query(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
	rows, err := c.client.Query(ctx, connection, sql, parameters)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, sqlEntry{database: connection.Database, sql: sql, params: maps.Clone(parameters), err: err})
	return rows, err
}

// take returns the calls recorded so far and starts a new transcript.
func (c *recordingClient) take() []sqlEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries := c.entries
	c.entries = nil
	return entries
}

// errUnsupportedRenderer marks a harness misuse rather than a render error worth recording.
var errUnsupportedRenderer = errors.New("unsupported golden renderer type")

// goldenStatements normalizes the supported renderer shapes; an empty single statement means none.
func goldenStatements(render any) ([]string, error) {
	var statement string
	var err error
	switch f := render.(type) {
	case func() string:
		statement = f()
	case func() []string:
		return f(), nil
	case func() (string, error):
		statement, err = f()
	case func() ([]string, error):
		statements, err := f()
		if err != nil {
			return nil, err
		}
		return statements, nil
	default:
		return nil, fmt.Errorf("%w %T", errUnsupportedRenderer, render)
	}
	if err != nil || statement == "" {
		return nil, err
	}
	return []string{statement}, nil
}

// goldenComment prefixes every line so multi-line messages cannot be mistaken for SQL.
func goldenComment(label, text string) string {
	return "-- " + label + ": " + strings.ReplaceAll(text, "\n", "\n-- ")
}

// formatSQL renders statements or a render error in the golden file format.
func formatSQL(statements []string, renderErr error) string {
	switch {
	case renderErr != nil:
		return goldenComment("error", renderErr.Error()) + "\n"
	case len(statements) == 0:
		return "-- no statements\n"
	}
	blocks := make([]string, 0, len(statements))
	for _, statement := range statements {
		blocks = append(blocks, statement+";")
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

// formatParams renders bindings as sorted compact JSON without HTML escaping, so values read as sent.
func formatParams(parameters map[string]string) string {
	if parameters == nil {
		parameters = map[string]string{}
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(parameters) // String maps are always JSON-serializable.
	return strings.TrimSuffix(buffer.String(), "\n")
}

// formatTranscript renders recorded calls with their database, bindings, and any client error.
func formatTranscript(entries []sqlEntry) string {
	if len(entries) == 0 {
		return "-- no statements\n"
	}
	blocks := make([]string, 0, len(entries))
	for _, entry := range entries {
		block := goldenComment("database", entry.database) + "\n" + entry.sql + ";\n" + goldenComment("params", formatParams(entry.params))
		if entry.err != nil {
			block += "\n" + goldenComment("error", entry.err.Error())
		}
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

// checkStatement enforces the statement shape that serialization and the golden format rely on.
func checkStatement(sql string, parameters map[string]string) error {
	if sql == "" || strings.TrimSpace(sql) != sql || strings.HasSuffix(sql, ";") {
		return fmt.Errorf("statement %q must be nonempty, trimmed, and unterminated", sql)
	}
	verb, _, _ := strings.Cut(sql, " ")
	switch {
	case slices.Contains(ddlVerbs, verb):
		// Redshift cannot bind parameters in DDL; a binding there means a value escaped quoting review.
		if len(parameters) != 0 {
			return fmt.Errorf("DDL statement %q must not carry bind parameters", sql)
		}
	case slices.Contains(readVerbs, verb):
	default:
		return fmt.Errorf("statement %q must start with one of %v or %v", sql, ddlVerbs, readVerbs)
	}
	return nil
}

// checkGoldenName rejects names that would not round-trip through the file system.
func checkGoldenName(kind, name string) error {
	if !goldenName.MatchString(name) {
		return fmt.Errorf("golden %s %q must match %s", kind, name, goldenName)
	}
	return nil
}

// checkGoldenGroup validates every slash-separated directory of a group.
func checkGoldenGroup(group string) error {
	for segment := range strings.SplitSeq(group, "/") {
		if err := checkGoldenName("group segment", segment); err != nil {
			return err
		}
	}
	return nil
}

// goldenPath maps a group and case to its file under testdata/sql.
func goldenPath(group, name string) string {
	return filepath.Join("testdata", "sql", filepath.FromSlash(group), name+".sql")
}

// updateCommand reproduces the exact go test invocation that rewrites the current test's golden files.
func updateCommand(t *testing.T) string {
	t.Helper()
	parts := strings.Split(t.Name(), "/")
	for index, part := range parts {
		parts[index] = "^" + regexp.QuoteMeta(part) + "$"
	}
	return "go test ./internal/provider -run '" + strings.Join(parts, "/") + "' -update"
}

// updating reports whether golden files should be rewritten; CI must compare against reviewed files only.
func updating(t *testing.T) bool {
	t.Helper()
	if *updateGolden && os.Getenv("CI") != "" {
		t.Fatal("refusing -update while CI is set; regenerate golden files locally and commit them")
	}
	return *updateGolden
}

// checkGolden compares or rewrites one golden file.
func checkGolden(t *testing.T, group, name, content string) {
	t.Helper()
	path := goldenPath(group, name)
	if updating(t) {
		if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
			return
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		return
	}
	expected, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		assert.Failf(t, "missing golden file", "%s does not exist; record it with: %s", path, updateCommand(t))
		return
	}
	require.NoError(t, err)
	assert.Equal(t, string(expected), content, "%s differs; if the change is intended, run: %s", path, updateCommand(t))
}

// checkStale reports golden files in a group that no case produced, deleting them on -update.
func checkStale(t *testing.T, group string, names map[string]bool) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "sql", filepath.FromSlash(group), "*.sql"))
	require.NoError(t, err)
	for _, file := range files {
		if names[strings.TrimSuffix(filepath.Base(file), ".sql")] {
			continue
		}
		if updating(t) {
			require.NoError(t, os.Remove(file))
			continue
		}
		assert.Failf(t, "stale golden file", "%s has no case in group %q; remove it with: %s", file, group, updateCommand(t))
	}
}

// checkSQL compares every renderer case of a group with testdata/sql/<group>/<case>.sql.
func checkSQL(t *testing.T, group string, cases []sqlCase) {
	t.Helper()
	require.NoError(t, checkGoldenGroup(group))
	names := map[string]bool{}
	for _, test := range cases {
		require.NoError(t, checkGoldenName("case", test.name))
		require.False(t, names[test.name], "duplicate golden case %q in group %q", test.name, group)
		names[test.name] = true
		statements, renderErr := goldenStatements(test.render)
		require.NotErrorIs(t, renderErr, errUnsupportedRenderer, "case %q", test.name)
		for _, statement := range statements {
			require.NoError(t, checkStatement(statement, nil), "case %q", test.name)
		}
		checkGolden(t, group, test.name, formatSQL(statements, renderErr))
	}
	if !t.Failed() {
		checkStale(t, group, names)
	}
}

// transcriptGroup tracks the cases one test records into a group, so stale files are detectable afterwards.
type transcriptGroup struct {
	// owner is the test whose cleanup checks the group for stale files.
	owner *testing.T
	// names lists the cases recorded so far.
	names map[string]bool
}

// transcriptGroups maps each group to the test recording it.
var transcriptGroups = struct {
	sync.Mutex
	groups map[string]*transcriptGroup
}{groups: map[string]*transcriptGroup{}}

// checkTranscript compares recorded calls with testdata/sql/<group>/<name>.sql.
// All cases of a group must be recorded through the same *testing.T; stale files are checked when it finishes.
func checkTranscript(t *testing.T, group, name string, entries []sqlEntry) {
	t.Helper()
	require.NoError(t, checkGoldenGroup(group))
	require.NoError(t, checkGoldenName("case", name))
	transcriptGroups.Lock()
	recorded, ok := transcriptGroups.groups[group]
	if !ok {
		recorded = &transcriptGroup{owner: t, names: map[string]bool{}}
		transcriptGroups.groups[group] = recorded
		t.Cleanup(func() {
			transcriptGroups.Lock()
			delete(transcriptGroups.groups, group)
			transcriptGroups.Unlock()
			// A failed or partial run has not recorded every case, so missing names are not evidence of staleness.
			if !t.Failed() && !t.Skipped() {
				checkStale(t, group, recorded.names)
			}
		})
	}
	owner, duplicate := recorded.owner, recorded.names[name]
	recorded.names[name] = true
	transcriptGroups.Unlock()
	require.Same(t, owner, t, "transcript group %q is already recorded by %s", group, owner.Name())
	require.False(t, duplicate, "duplicate transcript %q in group %q", name, group)
	for _, entry := range entries {
		require.NoError(t, checkStatement(entry.sql, entry.params), "transcript %s/%s", group, name)
	}
	checkGolden(t, group, name, formatTranscript(entries))
}

// TestGoldenRenderForms checks every supported renderer shape and rejects others.
func TestGoldenRenderForms(t *testing.T) {
	failure := errors.New("invalid target")
	for _, test := range []struct {
		name       string
		render     any
		statements []string
		err        error
	}{
		{"string", func() string { return "DROP ROLE x" }, []string{"DROP ROLE x"}, nil},
		{"empty string", func() string { return "" }, nil, nil},
		{"strings", func() []string { return []string{"ALTER USER x CREATEDB", "ALTER USER x CREATEUSER"} }, []string{"ALTER USER x CREATEDB", "ALTER USER x CREATEUSER"}, nil},
		{"string error", func() (string, error) { return "", failure }, nil, failure},
		{"string value", func() (string, error) { return "CREATE ROLE x", nil }, []string{"CREATE ROLE x"}, nil},
		{"strings error", func() ([]string, error) { return []string{"ignored"}, failure }, nil, failure},
		{"strings value", func() ([]string, error) { return nil, nil }, nil, nil},
		{"unsupported", func() int { return 0 }, nil, errUnsupportedRenderer},
	} {
		t.Run(test.name, func(t *testing.T) {
			statements, err := goldenStatements(test.render)
			assert.Equal(t, test.statements, statements)
			if test.err == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, test.err)
			}
		})
	}
}

// TestGoldenFormat pins the file format that reviewers and the refactor gate read.
func TestGoldenFormat(t *testing.T) {
	assert.Equal(t, "-- no statements\n", formatSQL(nil, nil))
	assert.Equal(t, "-- error: first\n-- second\n", formatSQL([]string{"DROP ROLE x"}, errors.New("first\nsecond")))
	assert.Equal(t, "CREATE ROLE x;\n\nDROP ROLE x;\n", formatSQL([]string{"CREATE ROLE x", "DROP ROLE x"}, nil))
	assert.Equal(t, "-- no statements\n", formatTranscript(nil))
	assert.Equal(t,
		"-- database: admin\nSELECT 1 WHERE a = :b AND c = :a;\n-- params: {\"a\":\"<x>\",\"b\":\"y\"}\n\n"+
			"-- database: analytics\nDROP ROLE x;\n-- params: {}\n-- error: denied\n",
		formatTranscript([]sqlEntry{
			{database: "admin", sql: "SELECT 1 WHERE a = :b AND c = :a", params: map[string]string{"b": "y", "a": "<x>"}},
			{database: "analytics", sql: "DROP ROLE x", err: errors.New("denied")},
		}))
}

// TestGoldenValidation checks statement verbs, bind placement, and file-name rules.
func TestGoldenValidation(t *testing.T) {
	for _, sql := range []string{"ALTER USER x", "COMMENT ON ROLE x IS NULL", "CREATE ROLE x", "DROP ROLE x", "GRANT USAGE ON SCHEMA x TO y", "REVOKE USAGE ON SCHEMA x FROM y", "SHOW GRANTS ON SCHEMA x"} {
		require.NoError(t, checkStatement(sql, nil), sql)
	}
	require.NoError(t, checkStatement("SELECT 1 WHERE a = :a", map[string]string{"a": "b"}))
	for _, sql := range []string{"", " DROP ROLE x", "DROP ROLE x;", "drop role x", "TRUNCATE x", "WITH x AS (SELECT 1) SELECT 1"} {
		require.Error(t, checkStatement(sql, nil), sql)
	}
	require.Error(t, checkStatement("DROP ROLE :name", map[string]string{"name": "x"}))
	require.NoError(t, checkGoldenGroup("lifecycle/role_grant"))
	for _, group := range []string{"", "Lifecycle", "lifecycle/", "life-cycle", "../x"} {
		require.Error(t, checkGoldenGroup(group), group)
	}
	require.Error(t, checkGoldenName("case", "create user"))
	assert.Equal(t, filepath.Join("testdata", "sql", "lifecycle", "role", "create.sql"), goldenPath("lifecycle/role", "create"))
	assert.Equal(t, "go test ./internal/provider -run '^TestGoldenValidation$' -update", updateCommand(t))
}
