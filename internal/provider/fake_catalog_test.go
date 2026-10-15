package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// literals extracts SQL string literals, preserving doubled quote escapes for fake catalog parsing.
var literals = regexp.MustCompile(`'((?:[^']|'')*)'`)

// catalog exercises real provider RPCs and Terraform plans without AWS.
type catalog struct {
	// mu serializes fake reads and writes during concurrent Terraform operations.
	mu sync.Mutex
	// group records whether the SQL user group exists.
	group bool
	// groupMember records the managed user-to-group relationship.
	groupMember bool
	// role records whether the SQL role exists.
	role bool
	// membership records the managed role-to-role grant.
	membership bool
	// user records whether the database user exists.
	user bool
	// userGrant records the managed role-to-user grant.
	userGrant bool
	// superuser records the user's CREATEUSER capability.
	superuser bool
	// createDB records the user's CREATEDB capability.
	createDB bool
	// database records whether the shared consumer database exists.
	database bool
	// localDB records whether the producer local database exists.
	localDB bool
	// permissions records the consumer WITH PERMISSIONS mode.
	permissions bool
	// share records whether the outbound datashare exists.
	share bool
	// schema records whether the local source schema exists.
	schema bool
	// external records whether the Glue schema mapping exists.
	external bool
	// shareSchema records explicit share/schema membership.
	shareSchema bool
	// includeNew records automatic inclusion of future schema objects.
	includeNew bool
	// shareTable records explicit share/table membership.
	shareTable bool
	// shareGrant records SQL usage granted to the consumer account.
	shareGrant bool
	// shareNamespaceGrant records SQL usage granted to a consumer namespace.
	shareNamespaceGrant bool
	// public records outbound share public accessibility.
	public bool
	// identity records whether the SQL identity provider exists.
	identity bool
	// enabled records identity-provider availability.
	enabled bool
	// iamRole records the identity-provider integration role.
	iamRole string
	// privileges records explicit scoped permissions on the test role.
	privileges map[string]bool
	// writes records mutations for no-write planning and ordering assertions.
	writes []string
	// roleName records the active role identity for name-change replacement tests.
	roleName string
	// databaseName records the active consumer identity for name-change replacement tests.
	databaseName string
	// families holds the state of registered fake families, created on first use.
	families map[string]fakeFamily
}

// Query emulates SQL catalog reads and mutations while enforcing lifecycle dependencies.
func (c *catalog) Query(_ context.Context, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if connection.Database == "" {
		return nil, fmt.Errorf("unknown connection reached SQL execution")
	}
	for _, name := range slices.Sorted(maps.Keys(fakeFamilies)) {
		rows, handled, err := c.family(name).query(c, connection, sql, parameters)
		if !handled {
			continue
		}
		if err == nil && !fakeRead(sql) {
			c.writes = append(c.writes, sql)
		}
		return rows, err
	}
	roleName := c.roleName
	if roleName == "" {
		roleName = "example:readers"
	}
	databaseName := c.databaseName
	if databaseName == "" {
		databaseName = "analytics"
	}
	switch {
	case strings.HasPrefix(sql, "SELECT database_name FROM svv_redshift_databases"):
		if c.localDB {
			return []dataapi.Row{{"database_name": parameters["database"]}}, nil
		}
	case strings.HasPrefix(sql, "SELECT g.groname"):
		if c.group && c.groupMember {
			return []dataapi.Row{{"groname": "readers"}}, nil
		}
	case strings.HasPrefix(sql, "SELECT groname"):
		if c.group {
			return []dataapi.Row{{"groname": "readers"}}, nil
		}
	case strings.HasPrefix(sql, "CREATE GROUP"):
		c.group = true
	case strings.HasPrefix(sql, "DROP GROUP"):
		c.group = false
	case strings.HasPrefix(sql, "ALTER GROUP"):
		c.groupMember = strings.Contains(sql, " ADD USER ")
	case strings.HasPrefix(sql, "SELECT name, type"):
		if c.identity {
			return []dataapi.Row{{"name": "identity", "type": "awsidc", "namespc": "example", "instanceid": "application", "params": fmt.Sprintf(`{"iam_role":%q}`, c.iamRole), "enabled": fmt.Sprint(c.enabled)}}, nil
		}
	case strings.HasPrefix(sql, "SELECT usename"):
		if strings.Contains(sql, "usesuper") && c.user {
			return []dataapi.Row{{"usename": "grafana", "usesuper": fmt.Sprint(c.superuser), "usecreatedb": fmt.Sprint(c.createDB)}}, nil
		}
		return nil, nil
	case strings.HasPrefix(sql, "SELECT role_name FROM svv_user_grants"):
		if c.user && c.userGrant {
			return []dataapi.Row{{"role_name": "sys:monitor"}}, nil
		}
	case strings.HasPrefix(sql, "SELECT share_name, share_type"):
		if c.share {
			return []dataapi.Row{{"share_name": "producer", "share_type": "OUTBOUND", "source_database": connection.Database, "managed_by": "", "is_publicaccessible": fmt.Sprint(c.public)}}, nil
		}
	case strings.HasPrefix(sql, "SELECT object_name") && strings.Contains(sql, "FROM svv_datashare_objects"):
		if parameters["schema"] == "serving" && c.shareSchema {
			return []dataapi.Row{{"object_name": "serving", "include_new": fmt.Sprint(c.includeNew)}}, nil
		}
		if parameters["object"] == "serving.table" && c.shareTable {
			return []dataapi.Row{{"object_name": "serving.table"}}, nil
		}
	case strings.HasPrefix(sql, "SELECT consumer_account, consumer_namespace FROM svv_datashare_consumers"):
		if c.shareGrant && parameters["account"] == "123456789012" && strings.Contains(sql, "NVL(consumer_namespace, '') = ''") {
			return []dataapi.Row{{"consumer_account": "123456789012", "consumer_namespace": ""}}, nil
		}
		if c.shareNamespaceGrant && parameters["namespace"] == "12345678-1234-1234-1234-123456789abc" && strings.Contains(sql, "consumer_namespace = :namespace") {
			return []dataapi.Row{{"consumer_account": "123456789012", "consumer_namespace": parameters["namespace"]}}, nil
		}
	case strings.HasPrefix(sql, "SELECT n.nspname AS schema_name"):
		if c.schema {
			return []dataapi.Row{{"schema_name": "serving", "owner": "admin"}}, nil
		}
	case strings.HasPrefix(sql, "SELECT schemaname, eskind, databasename, esoptions"):
		if c.external {
			return []dataapi.Row{{"schemaname": "example_external", "eskind": "1", "databasename": "example_glue", "esoptions": `{"IAM_ROLE":"arn:aws:iam::123456789012:role/spectrum"}`}}, nil
		}
	case strings.HasPrefix(sql, "SHOW DATABASES"):
		name, err := literal(sql, 0)
		if err != nil {
			return nil, err
		}
		if name == "warehouse" && c.localDB {
			return []dataapi.Row{{"database_name": "warehouse", "database_type": "local"}}, nil
		}
		if c.database && name == databaseName {
			options := fmt.Sprintf(`{"datashare_name":"source","datashare_producer_account":"123456789012","datashare_producer_namespace":"11111111-2222-3333-4444-555555555555","permissions":%t}`, c.permissions)
			return []dataapi.Row{{"database_name": databaseName, "database_type": "shared", "parameters": options}}, nil
		}
	case strings.HasPrefix(sql, "SELECT consumer_database"):
		name := ""
		if c.database {
			name = databaseName
		}
		return []dataapi.Row{{"consumer_database": name}}, nil
	case strings.HasPrefix(sql, "SELECT database_type"):
		if c.database {
			return []dataapi.Row{{"database_type": "shared"}}, nil
		}
	case strings.HasPrefix(sql, "SELECT role_name FROM svv_role_grants"):
		if c.role && c.membership {
			return []dataapi.Row{{"role_name": roleName}}, nil
		}
	case strings.HasPrefix(sql, "SELECT role_name FROM svv_roles"):
		if c.role {
			return []dataapi.Row{{"role_name": roleName}}, nil
		}
	case strings.HasPrefix(sql, "SHOW GRANTS"):
		var rows []dataapi.Row
		for privilege := range c.privileges {
			rows = append(rows, dataapi.Row{"database_name": databaseName, "identity_name": roleName, "object_type": "DATABASE", "privilege_scope": "TABLES", "privilege_type": privilege})
		}
		return rows, nil
	case strings.HasPrefix(sql, "CREATE IDENTITY PROVIDER"):
		c.identity, c.enabled = true, true
		role, err := literal(sql, 2)
		if err != nil {
			return nil, err
		}
		c.iamRole = role
	case strings.HasPrefix(sql, "ALTER IDENTITY PROVIDER"):
		if strings.Contains(sql, " IAM_ROLE ") {
			role, err := literal(sql, 0)
			if err != nil {
				return nil, err
			}
			c.iamRole = role
		} else {
			c.enabled = strings.HasSuffix(sql, " ENABLE")
		}
	case strings.HasPrefix(sql, "DROP IDENTITY PROVIDER"):
		if c.role {
			return nil, fmt.Errorf("role dependency still exists")
		}
		c.identity = false
	case strings.HasPrefix(sql, "CREATE ROLE"):
		if c.role || !c.identity {
			return nil, fmt.Errorf("role exists or identity provider is absent")
		}
		c.role = true
		c.roleName = strings.Trim(strings.TrimPrefix(sql, "CREATE ROLE "), `"`)
	case strings.HasPrefix(sql, "DROP ROLE"):
		if c.membership || len(c.privileges) > 0 {
			return nil, fmt.Errorf("role still has dependencies")
		}
		c.role = false
	case strings.HasPrefix(sql, "CREATE USER"):
		if c.user {
			return nil, fmt.Errorf("user already exists")
		}
		c.user = true
		c.superuser = strings.Contains(sql, " CREATEUSER")
		c.createDB = strings.Contains(sql, " CREATEDB")
	case strings.HasPrefix(sql, "ALTER USER"):
		switch {
		case strings.HasSuffix(sql, " NOCREATEUSER"):
			c.superuser = false
		case strings.HasSuffix(sql, " CREATEUSER"):
			c.superuser = true
		case strings.HasSuffix(sql, " NOCREATEDB"):
			c.createDB = false
		case strings.HasSuffix(sql, " CREATEDB"):
			c.createDB = true
		}
	case strings.HasPrefix(sql, "DROP USER"):
		if c.userGrant {
			return nil, fmt.Errorf("user still has role grants")
		}
		c.user = false
	case strings.HasPrefix(sql, "CREATE DATABASE"):
		if sql == `CREATE DATABASE "warehouse"` {
			c.localDB = true
			break
		}
		if c.database {
			return nil, fmt.Errorf("database already exists")
		}
		c.database, c.permissions = true, strings.Contains(sql, "WITH PERMISSIONS")
		quoted := strings.Split(sql, `"`)
		if len(quoted) < 2 {
			return nil, fmt.Errorf("fake catalog cannot parse database name from %q", sql)
		}
		c.databaseName = quoted[1]
	case strings.HasPrefix(sql, "DROP DATABASE"):
		if sql == `DROP DATABASE "warehouse"` {
			if c.share {
				return nil, fmt.Errorf("datashare still uses the producer database")
			}
			c.localDB = false
			break
		}
		if len(c.privileges) > 0 {
			return nil, fmt.Errorf("database still has grants")
		}
		c.database = false
	case strings.HasPrefix(sql, "CREATE DATASHARE"):
		if c.share {
			return nil, fmt.Errorf("datashare already exists")
		}
		c.share, c.public = true, strings.HasSuffix(sql, "true")
	case strings.HasPrefix(sql, "ALTER DATASHARE") && strings.Contains(sql, " SET PUBLICACCESSIBLE "):
		c.public = strings.HasSuffix(sql, "true")
	case strings.HasPrefix(sql, "DROP DATASHARE"):
		if c.shareSchema || c.shareGrant {
			return nil, fmt.Errorf("datashare has members or consumers")
		}
		c.share = false
	case strings.HasPrefix(sql, "ALTER DATASHARE") && strings.Contains(sql, " ADD SCHEMA "):
		c.shareSchema = true
	case strings.HasPrefix(sql, "ALTER DATASHARE") && strings.Contains(sql, " SET INCLUDENEW "):
		c.includeNew = strings.Contains(sql, " SET INCLUDENEW TRUE ") || strings.Contains(sql, " SET INCLUDENEW true ")
	case strings.HasPrefix(sql, "ALTER DATASHARE") && strings.Contains(sql, " REMOVE SCHEMA "):
		if c.shareTable {
			return nil, fmt.Errorf("datashare schema contains tables")
		}
		c.shareSchema = false
	case strings.HasPrefix(sql, "ALTER DATASHARE") && strings.Contains(sql, " ADD TABLE "):
		c.shareTable = true
	case strings.HasPrefix(sql, "ALTER DATASHARE") && strings.Contains(sql, " REMOVE TABLE "):
		c.shareTable = false
	case strings.HasPrefix(sql, "GRANT USAGE ON DATASHARE"):
		if strings.Contains(sql, " TO NAMESPACE ") {
			c.shareNamespaceGrant = true
		} else {
			c.shareGrant = true
		}
	case strings.HasPrefix(sql, "REVOKE USAGE ON DATASHARE"):
		if strings.Contains(sql, " FROM NAMESPACE ") {
			c.shareNamespaceGrant = false
		} else {
			c.shareGrant = false
		}
	case strings.HasPrefix(sql, "CREATE SCHEMA"):
		c.schema = true
	case strings.HasPrefix(sql, "DROP SCHEMA"):
		if strings.Contains(sql, "example_external") {
			c.external = false
		} else {
			c.schema = false
		}
	case strings.HasPrefix(sql, "CREATE EXTERNAL SCHEMA"):
		c.external = true
	case strings.HasPrefix(sql, "GRANT ROLE"):
		if strings.Contains(sql, " TO ROLE ") {
			c.membership = true
		} else {
			c.userGrant = true
		}
	case strings.HasPrefix(sql, "REVOKE ROLE"):
		if strings.Contains(sql, " FROM ROLE ") {
			c.membership = false
		} else {
			c.userGrant = false
		}
	case strings.HasPrefix(sql, "GRANT "):
		c.privileges[strings.Fields(sql)[1]] = true
	case strings.HasPrefix(sql, "REVOKE "):
		delete(c.privileges, strings.Fields(sql)[1])
	default:
		return nil, fmt.Errorf("unexpected SQL: %s (%v)", sql, parameters)
	}
	if !fakeRead(sql) {
		c.writes = append(c.writes, sql)
	}
	return nil, nil
}

// literal returns the index-th SQL string literal, failing the fake query instead of panicking on unexpected SQL.
func literal(sql string, index int) (string, error) {
	values := literals.FindAllStringSubmatch(sql, -1)
	if index >= len(values) {
		return "", fmt.Errorf("fake catalog expected at least %d string literals in %q", index+1, sql)
	}
	return values[index][1], nil
}

// fakeFamily emulates the catalog of one object family in its own file, so new types extend the fake
// without editing the legacy switch.
type fakeFamily interface {
	// query answers or applies sql while the catalog lock is held; handled=false passes it to the next
	// family and then to the legacy switch. Use c.family to reach another family's state.
	query(c *catalog, connection dataapi.Connection, sql string, parameters map[string]string) (rows []dataapi.Row, handled bool, err error)
	// populate makes the family's representative objects exist, as fullCatalog does for legacy state.
	populate()
}

// fakeFamilies maps each registered family name to the factory of its per-catalog state.
var fakeFamilies = map[string]func() fakeFamily{}

// registerFakeFamily adds a family to every fake catalog; it returns true for `var _ = ...` declarations.
func registerFakeFamily(name string, factory func() fakeFamily) bool {
	if _, ok := fakeFamilies[name]; ok {
		panic("fake family " + name + " is registered twice")
	}
	fakeFamilies[name] = factory
	return true
}

// family returns the named family's state, creating it on first use. Callers hold c.mu or have exclusive
// access to c, as query handlers and catalog constructors do.
func (c *catalog) family(name string) fakeFamily {
	if c.families == nil {
		c.families = map[string]fakeFamily{}
	}
	state, ok := c.families[name]
	if !ok {
		factory, registered := fakeFamilies[name]
		if !registered {
			panic("fake family " + name + " is not registered")
		}
		state = factory()
		c.families[name] = state
	}
	return state
}

// populate fills every registered family, in name order so populate side effects are deterministic.
func (c *catalog) populate() {
	for _, name := range slices.Sorted(maps.Keys(fakeFamilies)) {
		c.family(name).populate()
	}
}

// fakeState returns a family's typed state for test setup and assertions outside query handlers.
func fakeState[F fakeFamily](c *catalog, name string) F {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.family(name).(F)
}

// fakeRead reports catalog reads, which the fake does not record as writes.
func fakeRead(sql string) bool {
	fields := strings.Fields(sql)
	return len(fields) > 0 && slices.Contains(readVerbs, fields[0])
}

// probeFamily is a minimal family for testing the dispatcher; it also overrides one legacy statement.
type probeFamily struct {
	// exists records whether the probe object exists.
	exists bool
	// roleSeen records whether the family saw the legacy role read before the switch did.
	roleSeen bool
}

// query handles probe statements and observes, without handling, the legacy role read.
func (f *probeFamily) query(c *catalog, _ dataapi.Connection, sql string, _ map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT probe"):
		if f.exists && c.role {
			return []dataapi.Row{{"probe": "exists"}}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "CREATE PROBE"):
		if f.exists {
			return nil, true, errors.New("probe already exists")
		}
		f.exists = true
		return nil, true, nil
	case strings.HasPrefix(sql, "DESC PROBE"):
		return []dataapi.Row{{"probe": "described"}}, true, nil
	case strings.HasPrefix(sql, "SELECT role_name FROM svv_roles"):
		f.roleSeen = true
	}
	return nil, false, nil
}

// populate makes the probe exist in a full catalog.
func (f *probeFamily) populate() { f.exists = true }

// TestFakeCatalogFamilies checks dispatch order, write recording, population, and typed access.
func TestFakeCatalogFamilies(t *testing.T) {
	require.True(t, registerFakeFamily("probe", func() fakeFamily { return &probeFamily{} }))
	t.Cleanup(func() { delete(fakeFamilies, "probe") })
	assert.Panics(t, func() { registerFakeFamily("probe", func() fakeFamily { return &probeFamily{} }) })
	ctx, connection := context.Background(), dataapi.Connection{Database: "admin"}

	full := fullCatalog()
	assert.True(t, fakeState[*probeFamily](full, "probe").exists, "fullCatalog populates registered families")
	rows, err := full.Query(ctx, connection, "SELECT probe", nil)
	require.NoError(t, err)
	assert.Equal(t, []dataapi.Row{{"probe": "exists"}}, rows)
	_, err = full.Query(ctx, connection, "CREATE PROBE x", nil)
	require.Error(t, err)
	assert.Empty(t, full.writes, "failed writes are not recorded")

	empty := &catalog{role: true}
	assert.False(t, fakeState[*probeFamily](empty, "probe").exists, "families start empty outside fullCatalog")
	_, err = empty.Query(ctx, connection, "CREATE PROBE x", nil)
	require.NoError(t, err)
	rows, err = empty.Query(ctx, connection, "DESC PROBE x", nil)
	require.NoError(t, err)
	assert.Equal(t, []dataapi.Row{{"probe": "described"}}, rows)
	rows, err = empty.Query(ctx, connection, "SELECT role_name FROM svv_roles WHERE role_name = :name", nil)
	require.NoError(t, err)
	assert.Equal(t, []dataapi.Row{{"role_name": "example:readers"}}, rows, "unhandled statements reach the legacy switch")
	assert.True(t, fakeState[*probeFamily](empty, "probe").roleSeen, "families are consulted before the legacy switch")
	assert.Equal(t, []string{"CREATE PROBE x"}, empty.writes)
	assert.Panics(t, func() { empty.family("unregistered") })
}
