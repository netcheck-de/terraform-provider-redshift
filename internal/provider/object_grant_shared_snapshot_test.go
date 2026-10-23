package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sharedSnapshotCatalog fakes a consumer database's shared members and their per-member ACLs.
type sharedSnapshotCatalog struct {
	// members lists the current shared object names.
	members []string
	// privileges records each member's held privileges.
	privileges map[string]map[string]bool
	// options records each member's privileges held with grant option.
	options map[string]map[string]bool
	// scope is the privilege_scope SHOW GRANTS reports.
	scope string
	// writes records the emitted mutations.
	writes []string
}

// Query emulates a superuser's view of a shared schema: member listings, per-member SHOW GRANTS rows, and GRANT or
// REVOKE applied to one member or to all of them.
func (c *sharedSnapshotCatalog) Query(_ context.Context, _ sqlclient.Connection, statement string, parameters map[string]string) ([]sqlclient.Row, error) {
	if strings.HasPrefix(statement, "SELECT usesuper") {
		return []sqlclient.Row{{"usesuper": "true"}}, nil
	}
	if parameters["collision_schema"] != "" || parameters["ambiguous_name"] != "" {
		return nil, nil
	}
	if strings.HasPrefix(statement, "SELECT DISTINCT") {
		var rows []sqlclient.Row
		for _, member := range c.members {
			row := sqlclient.Row{"object_name": member}
			if strings.Contains(statement, "function_type") {
				row["function_type"] = "STORED PROCEDURE"
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
	if strings.HasPrefix(statement, "SHOW GRANTS ON") {
		for _, member := range c.members {
			if !strings.HasSuffix(statement, sqlclient.Ident(member).String()) {
				continue
			}
			var rows []sqlclient.Row
			for privilege, held := range c.privileges[member] {
				if !held {
					continue
				}
				rows = append(rows, sqlclient.Row{"privilege_type": privilege, "identity_name": "reader", "identity_type": "user", "privilege_scope": c.scope, "admin_option": "false"})
				if c.options[member][privilege] {
					rows = append(rows, sqlclient.Row{"privilege_type": privilege, "identity_name": "reader", "identity_type": "user", "privilege_scope": c.scope, "admin_option": "true"})
				}
			}
			return rows, nil
		}
		return nil, fmt.Errorf("unexpected member read %s", statement)
	}
	if strings.HasPrefix(statement, "SELECT") {
		return []sqlclient.Row{{"name": "exists"}}, nil
	}
	c.writes = append(c.writes, statement)
	grant := strings.HasPrefix(statement, "GRANT ")
	optionOnly := strings.HasPrefix(statement, "REVOKE GRANT OPTION FOR ")
	_, rest, _ := strings.Cut(statement, " ")
	if optionOnly {
		rest = strings.TrimPrefix(rest, "GRANT OPTION FOR ")
	}
	privilege, _, _ := strings.Cut(rest, " ")
	for _, member := range c.members {
		if !strings.Contains(statement, "ON ALL") && !strings.Contains(statement, sqlclient.Ident(member).String()+" ") {
			continue
		}
		switch {
		case grant:
			c.privileges[member][privilege] = true
			c.options[member][privilege] = c.options[member][privilege] || strings.HasSuffix(statement, "WITH GRANT OPTION")
		case optionOnly:
			delete(c.options[member], privilege)
		default:
			delete(c.privileges[member], privilege)
			delete(c.options[member], privilege)
		}
	}
	return nil, nil
}

// TestObjectGrantSharedSnapshots grants an ALL-scope privilege on a shared schema member by member, revokes grant
// options held by only some members, clears every member on destroy, and owns nothing once the schema is empty.
func TestObjectGrantSharedSnapshots(t *testing.T) {
	for _, kind := range []string{"ALL TABLES", "ALL FUNCTIONS", "ALL PROCEDURES"} {
		t.Run(kind, func(t *testing.T) {
			privilege, scope := "SELECT", "TABLE"
			if kind != "ALL TABLES" {
				privilege, scope = "EXECUTE", "FUNCTION"
			}
			c := &sharedSnapshotCatalog{members: []string{"first", "second"}, privileges: map[string]map[string]bool{"first": {privilege: true}, "second": {}}, options: map[string]map[string]bool{"first": {privilege: true}, "second": {}}, scope: scope}
			r := newObjectGrantResource().(*privilegeResource)
			r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, connection sqlclient.Connection, statement string, parameters map[string]string) ([]sqlclient.Row, error) {
				assert.Equal(t, "admin", connection.Database)
				return c.Query(ctx, connection, statement, parameters)
			}))
			data := privilegeObject(t, r, map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": kind, "grantee": "reader", "grantee_type": "USER"}, privilege)
			actual := data
			_, found, err := r.read(context.Background(), &actual)
			require.NoError(t, err)
			require.True(t, found)
			assert.Empty(t, knownStrings(actual.Attributes()["privileges"].(types.Set)), "an incomplete snapshot has no common privilege")
			require.NoError(t, r.reconcile(context.Background(), data))
			for _, member := range c.members {
				assert.True(t, c.privileges[member][privilege])
				assert.False(t, c.options[member][privilege], "partial options are revoked, not hidden by the intersection")
			}
			require.NoError(t, r.reconcile(context.Background(), withGrantOptions(data, []string{privilege})))
			for _, member := range c.members {
				assert.True(t, c.options[member][privilege])
			}
			// Destroy must clear even privileges and options present on just one member.
			delete(c.privileges["second"], privilege)
			delete(c.options["second"], privilege)
			require.NoError(t, r.reconcile(context.Background(), withGrantOptions(withPrivileges(data, nil), nil)))
			for _, member := range c.members {
				assert.Empty(t, c.privileges[member])
				assert.Empty(t, c.options[member])
			}
			c.members = nil
			_, found, err = r.read(context.Background(), &actual)
			require.NoError(t, err)
			assert.False(t, found, "an empty snapshot has no objects to own")
		})
	}
}

// TestObjectGrantSharedExtendedSQL pins the visibility checks, reads, guards, and GRANT and REVOKE statements for
// routine and ALL-scope grants on shared databases.
func TestObjectGrantSharedExtendedSQL(t *testing.T) {
	var cases []sqlCase
	for _, test := range []struct{ name, kind, object, privilege string }{{"function", "FUNCTION", "routine", "EXECUTE"}, {"procedure", "PROCEDURE", "routine", "EXECUTE"}, {"all_tables", "ALL TABLES", "", "SELECT"}, {"all_functions", "ALL FUNCTIONS", "", "EXECUTE"}, {"all_procedures", "ALL PROCEDURES", "", "EXECUTE"}} {
		cases = append(cases, sqlCase{name: test.name, render: func() ([]string, error) {
			r := newObjectGrantResource().(*privilegeResource)
			data := privilegeObject(t, r, map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": test.kind, "object_name": test.object, "grantee": "reader", "grantee_type": "USER"})
			target, err := r.target(data)
			if err != nil {
				return nil, err
			}
			var statements []string
			if target.completeCatalog {
				statement, _, err := grantCatalogVisibilityQuery().Build()
				if err != nil {
					return nil, err
				}
				statements = append(statements, statement)
			}
			for _, check := range target.checks {
				if check.sql != "" {
					statements = append(statements, check.sql)
				}
			}
			if target.snapshot != nil {
				statements = append(statements, target.snapshot.members.sql, target.snapshot.query(sqlclient.Row{"object_name": "member"}).sql)
			} else {
				statements = append(statements, target.query.sql)
			}
			if target.mutationGuard != nil {
				statements = append(statements, target.mutationGuard.sql)
			}
			if target.snapshot != nil && target.snapshot.grant != nil {
				target.grant = target.snapshot.grant(sqlclient.Row{"object_name": "member"})
			}
			privilege, err := sqlclient.OneOf(test.privilege, target.allowed...)
			if err != nil {
				return nil, err
			}
			grant, err := target.grant.optionStatement(true, privilege)
			if err != nil {
				return nil, err
			}
			revokeOption, err := target.grant.optionStatement(false, privilege)
			if err != nil {
				return nil, err
			}
			return append(statements, grant, revokeOption, target.grant.statement(false, privilege)), nil
		}})
	}
	checkSQL(t, "object_grant_shared_extended", cases)
}

// TestObjectGrantSharedFunctionIsNameWide rejects arguments for shared routines, whose grants cover every overload of a
// name, even when object_type is unknown.
func TestObjectGrantSharedFunctionIsNameWide(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	data := privilegeObject(t, r, map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": "FUNCTION", "object_name": "routine", "grantee": "reader", "grantee_type": "USER"}, "EXECUTE")
	require.NoError(t, r.validate(data))
	lookupValue(&data, "arguments", types.StringValue("INTEGER"))
	require.ErrorContains(t, r.validate(data), "name-wide")
	lookupValue(&data, "object_type", types.StringUnknown())
	require.ErrorContains(t, r.validateConfig(data), "name-wide", "the shared argument rule does not depend on an unknown object kind")
}

// TestObjectGrantSharedSnapshotReadErrors fails Read, without reporting the grant missing, when the member listing or a
// member's grants cannot be read or a member has no name.
func TestObjectGrantSharedSnapshotReadErrors(t *testing.T) {
	for _, failure := range []string{"members", "object", "empty name"} {
		t.Run(failure, func(t *testing.T) {
			r := newObjectGrantResource().(*privilegeResource)
			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, statement string, _ map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(statement, "SELECT usesuper") {
					return []sqlclient.Row{{"usesuper": "true"}}, nil
				}
				if strings.HasPrefix(statement, "SELECT DISTINCT") {
					switch failure {
					case "members":
						return nil, errors.New("member catalog unavailable")
					case "empty name":
						return []sqlclient.Row{{"object_name": ""}}, nil
					default:
						return []sqlclient.Row{{"object_name": "member"}}, nil
					}
				}
				if strings.HasPrefix(statement, "SHOW GRANTS") {
					return nil, errors.New("object catalog unavailable")
				}
				return []sqlclient.Row{{"name": "exists"}}, nil
			}))
			data := privilegeObject(t, r, map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": "ALL TABLES", "grantee": "reader", "grantee_type": "USER"})
			_, found, err := r.read(context.Background(), &data)
			require.Error(t, err)
			assert.False(t, found)
		})
	}
}

// TestObjectGrantSharedSnapshotTranscripts pins the per-member statements for changing grant options held by only some
// members and for deleting privileges held by only some.
func TestObjectGrantSharedSnapshotTranscripts(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	fields := map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": "ALL FUNCTIONS", "grantee": "reader", "grantee_type": "USER"}
	plain := privilegeObject(t, r, fields, "EXECUTE")
	option := withGrantOptions(plain, []string{"EXECUTE"})
	fresh := func(partial bool) func() sqlclient.Client {
		return func() sqlclient.Client {
			c := &sharedSnapshotCatalog{members: []string{"first", "second"}, privileges: map[string]map[string]bool{"first": {"EXECUTE": true}, "second": {"EXECUTE": true}}, options: map[string]map[string]bool{"first": {"EXECUTE": true}, "second": {}}, scope: "FUNCTION"}
			if partial {
				delete(c.privileges["second"], "EXECUTE")
			}
			return c
		}
	}
	runTranscripts(t, "object_grant_shared_transcript", newObjectGrantResource, []transcriptCase{
		{name: "upgrade_partial_options", operation: "update", catalog: fresh(false), prior: plain, planned: option},
		{name: "downgrade_partial_options", operation: "update", catalog: fresh(false), prior: option, planned: plain},
		{name: "delete_partial_privileges", operation: "delete", catalog: fresh(true), prior: plain},
	})
}

// TestObjectGrantSharedAmbiguousRoutineNames refuses to mutate a shared routine grant when another routine kind shares
// the name, because the consumer sees one name-wide ACL that neither kind can own alone.
func TestObjectGrantSharedAmbiguousRoutineNames(t *testing.T) {
	for _, kind := range []string{"FUNCTION", "PROCEDURE", "ALL PROCEDURES"} {
		t.Run(kind, func(t *testing.T) {
			r := newObjectGrantResource().(*privilegeResource)
			writes := 0
			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, statement string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(statement, "SELECT usesuper") {
					return []sqlclient.Row{{"usesuper": "true"}}, nil
				}
				if parameters["ambiguous_name"] != "" || parameters["collision_schema"] != "" {
					return []sqlclient.Row{{"function_name": "routine"}}, nil
				}
				if strings.HasPrefix(statement, "SELECT DISTINCT") {
					if kind == "ALL PROCEDURES" {
						return []sqlclient.Row{{"object_name": "routine", "function_type": "STORED PROCEDURE"}, {"object_name": "routine", "function_type": "REGULAR FUNCTION"}}, nil
					}
					return []sqlclient.Row{{"object_name": "routine"}}, nil
				}
				if strings.HasPrefix(statement, "SHOW GRANTS") {
					return []sqlclient.Row{{"privilege_type": "EXECUTE", "identity_name": "reader", "identity_type": "user", "privilege_scope": "FUNCTION"}}, nil
				}
				if strings.HasPrefix(statement, "SELECT") {
					return []sqlclient.Row{{"name": "exists"}}, nil
				}
				writes++
				return nil, nil
			}))
			fields := map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": kind, "grantee": "reader", "grantee_type": "USER"}
			if kind != "ALL PROCEDURES" {
				fields["object_name"] = "routine"
			}
			data := privilegeObject(t, r, fields, "EXECUTE")
			observed := data
			_, found, err := r.read(context.Background(), &observed)
			if kind == "ALL PROCEDURES" {
				require.ErrorContains(t, err, "name-wide ACL")
				assert.False(t, found)
				require.ErrorContains(t, r.reconcile(context.Background(), data), "name-wide ACL")
				assert.Zero(t, writes)
				return
			}
			require.NoError(t, err)
			require.True(t, found)
			require.ErrorContains(t, r.reconcile(context.Background(), data), "name-wide ACL")
			assert.Zero(t, writes, "ambiguous source kinds cannot independently own the same consumer permission")
		})
	}
}

// TestObjectGrantSharedCompleteVisibilityRequired requires a SQL superuser before reading routine or ALL-scope grants
// on a shared database, because other users see a filtered catalog.
func TestObjectGrantSharedCompleteVisibilityRequired(t *testing.T) {
	for _, kind := range []string{"FUNCTION", "PROCEDURE", "ALL TABLES", "ALL FUNCTIONS", "ALL PROCEDURES"} {
		t.Run(kind, func(t *testing.T) {
			r := newObjectGrantResource().(*privilegeResource)
			calls := 0
			r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, statement string, _ map[string]string) ([]sqlclient.Row, error) {
				calls++
				assert.True(t, strings.HasPrefix(statement, "SELECT usesuper"), "filtered catalog must not be consulted: %s", statement)
				return []sqlclient.Row{{"usesuper": "false"}}, nil
			}))
			fields := map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": kind, "grantee": "reader", "grantee_type": "USER"}
			if kind == "FUNCTION" || kind == "PROCEDURE" {
				fields["object_name"] = "routine"
			}
			data := privilegeObject(t, r, fields)
			_, found, err := r.read(context.Background(), &data)
			require.ErrorContains(t, err, "SQL superuser")
			assert.False(t, found)
			assert.Equal(t, 1, calls)
		})
	}
}

// TestSharedGrantSnapshotIgnoresOtherRecipients keeps only the managed grantee's rows from SHOW GRANTS.
func TestSharedGrantSnapshotIgnoresOtherRecipients(t *testing.T) {
	r := newObjectGrantResource().(*privilegeResource)
	r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		if strings.Contains(sql, "DISTINCT table_name AS object_name") {
			return []sqlclient.Row{{"object_name": "orders"}}, nil
		}
		assert.Equal(t, `SHOW GRANTS ON TABLE "shared_db"."serving"."orders"`, sql)
		return []sqlclient.Row{{"identity_name": "other", "identity_type": "user", "privilege_scope": "TABLE", "privilege_type": "SELECT"}, {"identity_name": "reader", "identity_type": "user", "privilege_scope": "TABLE", "privilege_type": "SELECT"}}, nil
	}))
	data := privilegeObject(t, r, map[string]string{"database_name": "shared_db", "database_type": "shared", "schema_name": "serving", "object_type": "ALL TABLES", "grantee": "reader", "grantee_type": "USER"})
	target, err := sharedObjectGrantTarget(data)
	require.NoError(t, err)
	rows, found, err := r.privilegeRows(context.Background(), target)
	require.NoError(t, err)
	assert.True(t, found)
	require.Len(t, rows, 1)
	assert.Equal(t, "reader", rows[0]["identity_name"])
}
