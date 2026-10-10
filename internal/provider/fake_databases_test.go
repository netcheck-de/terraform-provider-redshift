package provider

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerFakeFamily("databases", func() fakeFamily { return newDatabasesFamily() })

// databasesFamily extends the legacy database, schema, and external schema fake with owners, options, and quotas.
// Existence stays in the legacy flags (localDB, database, schema, external), which other types depend on.
type databasesFamily struct {
	// owner, connectionLimit, collation, and isolation are the catalog values of the local "warehouse" database.
	owner, connectionLimit, collation, isolation string
	// schemaOwner and schemaQuota are the owner and quota (empty for none) of the "serving" schema.
	schemaOwner, schemaQuota string
	// sessionUser and sessionRegular describe the connected user; a regular user sees only its own quota rows.
	sessionUser    string
	sessionRegular bool
	// externalKind, externalDatabase, externalOptions, and externalOwner describe the "example_external" schema.
	externalKind, externalDatabase, externalOwner string
	// externalOptions holds esoptions with upper-case keys.
	externalOptions map[string]string
	// externalHidden emulates a regular user whom SVV_EXTERNAL_SCHEMAS does not show the schema of another owner.
	externalHidden bool
}

// newDatabasesFamily returns the defaults a schema or database gets when created without options.
func newDatabasesFamily() *databasesFamily {
	f := &databasesFamily{schemaOwner: "admin", sessionUser: "admin"}
	f.resetDatabase()
	f.resetExternal()
	f.externalKind, f.externalDatabase = "1", "example_glue"
	f.externalOptions = map[string]string{"IAM_ROLE": "arn:aws:iam::123456789012:role/spectrum"}
	return f
}

// resetDatabase applies the Redshift defaults of CREATE DATABASE.
func (f *databasesFamily) resetDatabase() {
	f.owner, f.connectionLimit, f.collation, f.isolation = "admin", "UNLIMITED", "case_sensitive", "Snapshot Isolation"
}

// resetExternal applies the owner a newly created external schema has.
func (f *databasesFamily) resetExternal() {
	f.externalOwner = "admin"
}

// populate keeps the defaults; fullCatalog's legacy flags decide which objects exist.
func (f *databasesFamily) populate() {}

var (
	// fakeIdentifierAfter matches a keyword followed by a quoted identifier.
	fakeIdentifierAfter = func(keyword string) *regexp.Regexp {
		return regexp.MustCompile(keyword + ` "((?:[^"]|"")*)"`)
	}
	// fakeLiteralAfter matches a keyword followed by a string literal.
	fakeLiteralAfter = func(keyword string) *regexp.Regexp {
		return regexp.MustCompile(`(?:^| )` + keyword + ` '((?:[^']|'')*)'`)
	}
	fakeOwner      = fakeIdentifierAfter(`(?:OWNER TO|OWNER|AUTHORIZATION)`)
	fakeLimit      = regexp.MustCompile(`CONNECTION LIMIT (-?\d+|UNLIMITED)`)
	fakeCollation  = regexp.MustCompile(`COLLATE (\w+)`)
	fakeIsolation  = regexp.MustCompile(`ISOLATION LEVEL (\w+)`)
	fakeQuota      = regexp.MustCompile(`QUOTA (UNLIMITED|(\d+) MB)`)
	fakeExternalOf = regexp.MustCompile(`FROM (DATA CATALOG|HIVE METASTORE|POSTGRES|MYSQL|REDSHIFT|KINESIS|MSK) `)
	fakePort       = regexp.MustCompile(` PORT (\d+)`)
	fakeAuth       = regexp.MustCompile(` AUTHENTICATION (none|iam|mtls)`)
)

// fakeIdentifier returns the unquoted identifier the pattern captured, if any.
func fakeIdentifier(pattern *regexp.Regexp, sql string) (string, bool) {
	match := pattern.FindStringSubmatch(sql)
	if match == nil {
		return "", false
	}
	return strings.ReplaceAll(match[1], `""`, `"`), true
}

// fakeLiteral returns the unescaped literal following keyword, if any.
func fakeLiteral(keyword, sql string) (string, bool) {
	match := fakeLiteralAfter(keyword).FindStringSubmatch(sql)
	if match == nil {
		return "", false
	}
	return commentUnescaper.Replace(match[1]), true
}

// fakeExternalKinds maps the FROM keyword to its eskind.
var fakeExternalKinds = map[string]string{"DATA CATALOG": "1", "HIVE METASTORE": "2", "POSTGRES": "3", "MYSQL": "8", "REDSHIFT": "5", "KINESIS": "9", "MSK": "10"}

// applyExternalOptions records the literal and keyword options of CREATE or ALTER EXTERNAL SCHEMA.
func (f *databasesFamily) applyExternalOptions(sql string) {
	for _, key := range []string{"IAM_ROLE", "REGION", "URI", "SCHEMA", "SECRET_ARN", "AUTHENTICATION_ARN"} {
		if value, ok := fakeLiteral(key, sql); ok {
			f.externalOptions[key] = value
		}
	}
	if match := fakePort.FindStringSubmatch(sql); match != nil {
		f.externalOptions["PORT"] = match[1]
	}
	if match := fakeAuth.FindStringSubmatch(sql); match != nil {
		f.externalOptions["AUTHENTICATION"] = strings.ToUpper(match[1])
		// A new mode replaces the certificate source the statement does not repeat.
		for _, key := range []string{"SECRET_ARN", "AUTHENTICATION_ARN"} {
			if _, ok := fakeLiteral(key, sql); !ok {
				delete(f.externalOptions, key)
			}
		}
	}
}

// applyDatabaseOptions records the options of CREATE or ALTER DATABASE.
func (f *databasesFamily) applyDatabaseOptions(sql string) {
	if owner, ok := fakeIdentifier(fakeOwner, sql); ok {
		f.owner = owner
	}
	if match := fakeLimit.FindStringSubmatch(sql); match != nil {
		f.connectionLimit = match[1]
	}
	if match := fakeCollation.FindStringSubmatch(sql); match != nil {
		f.collation = strings.ToLower(match[1])
	}
	if match := fakeIsolation.FindStringSubmatch(sql); match != nil {
		f.isolation = map[string]string{"SNAPSHOT": "Snapshot Isolation", "SERIALIZABLE": "Serializable"}[match[1]]
	}
}

// applySchemaOptions records the owner and quota of CREATE or ALTER SCHEMA.
func (f *databasesFamily) applySchemaOptions(sql string) {
	if owner, ok := fakeIdentifier(fakeOwner, sql); ok {
		f.schemaOwner = owner
	}
	if match := fakeQuota.FindStringSubmatch(sql); match != nil {
		f.schemaQuota = match[2]
	}
}

// query answers the database, schema, and external schema statements of this block.
func (f *databasesFamily) query(c *catalog, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SHOW DATABASES"):
		name, err := literal(sql, 0)
		if err != nil {
			return nil, true, err
		}
		databaseName := c.databaseName
		if databaseName == "" {
			databaseName = "analytics"
		}
		if name == "warehouse" && c.localDB {
			return []dataapi.Row{{"database_name": "warehouse", "database_owner": "100", "database_type": "local", "database_isolation_level": f.isolation}}, true, nil
		}
		if c.database && name == databaseName {
			options := fmt.Sprintf(`{"datashare_name":"source","datashare_producer_account":"123456789012","datashare_producer_namespace":"11111111-2222-3333-4444-555555555555","permissions":%t}`, c.permissions)
			return []dataapi.Row{{"database_name": databaseName, "database_owner": "100", "database_type": "shared", "parameters": options, "database_isolation_level": "UNKNOWN"}}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT u.usename AS owner, d.datconnlimit"):
		if c.localDB && parameters["name"] == "warehouse" {
			return []dataapi.Row{{"owner": f.owner, "connection_limit": f.connectionLimit}}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT db_collation()"):
		if connection.Database != "warehouse" || !c.localDB {
			return nil, true, fmt.Errorf("database %q does not exist", connection.Database)
		}
		return []dataapi.Row{{"collation": f.collation}}, true, nil
	case strings.HasPrefix(sql, `CREATE DATABASE "warehouse"`):
		if c.localDB {
			return nil, true, fmt.Errorf("database already exists")
		}
		c.localDB = true
		f.resetDatabase()
		f.applyDatabaseOptions(sql)
		return nil, true, nil
	case strings.HasPrefix(sql, `ALTER DATABASE "warehouse" `):
		if !c.localDB {
			return nil, true, fmt.Errorf("database does not exist")
		}
		f.applyDatabaseOptions(strings.TrimPrefix(sql, `ALTER DATABASE "warehouse"`))
		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT n.nspname AS schema_name, u.usename AS owner FROM pg_namespace"):
		if c.schema {
			return []dataapi.Row{{"schema_name": "serving", "owner": f.schemaOwner}}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT quota FROM svv_redshift_schema_quota"):
		if c.schema && f.schemaQuota != "" && (!f.sessionRegular || f.schemaOwner == f.sessionUser) {
			return []dataapi.Row{{"quota": f.schemaQuota}}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT usename AS name, usesuper AS superuser FROM pg_user"):
		return []dataapi.Row{{"name": f.sessionUser, "superuser": fmt.Sprint(!f.sessionRegular)}}, true, nil
	case strings.HasPrefix(sql, "CREATE SCHEMA"):
		c.schema = true
		f.schemaOwner, f.schemaQuota = "admin", ""
		f.applySchemaOptions(strings.TrimPrefix(sql, "CREATE SCHEMA"))
		return nil, true, nil
	case strings.HasPrefix(sql, `ALTER SCHEMA "example_external" OWNER TO `):
		if owner, ok := fakeIdentifier(fakeOwner, sql); ok && c.external {
			f.externalOwner = owner
			return nil, true, nil
		}
		return nil, true, fmt.Errorf("external schema does not exist")
	case strings.HasPrefix(sql, "ALTER SCHEMA"):
		if !c.schema {
			return nil, true, fmt.Errorf("schema does not exist")
		}
		f.applySchemaOptions(strings.TrimPrefix(sql, "ALTER SCHEMA"))
		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT n.nspname AS schemaname, s.eskind"):
		if !c.external {
			return nil, true, nil
		}
		if f.externalHidden {
			return []dataapi.Row{{"schemaname": "example_external", "eskind": "", "databasename": "", "esoptions": "", "owner": f.externalOwner}}, true, nil
		}
		// Map keys marshal in sorted order, so transcripts stay deterministic.
		encoded, err := json.Marshal(f.externalOptions)
		if err != nil {
			return nil, true, err
		}
		return []dataapi.Row{{"schemaname": "example_external", "eskind": f.externalKind, "databasename": f.externalDatabase, "esoptions": string(encoded), "owner": f.externalOwner}}, true, nil
	case strings.HasPrefix(sql, "CREATE EXTERNAL SCHEMA"):
		if c.external {
			return nil, true, fmt.Errorf("external schema already exists")
		}
		match := fakeExternalOf.FindStringSubmatch(sql)
		if match == nil {
			return nil, true, fmt.Errorf("fake catalog cannot parse the source of %q", sql)
		}
		c.external = true
		f.resetExternal()
		f.externalKind, f.externalOptions = fakeExternalKinds[match[1]], map[string]string{}
		f.externalDatabase, _ = fakeLiteral("DATABASE", sql)
		f.applyExternalOptions(sql)
		return nil, true, nil
	case strings.HasPrefix(sql, "ALTER EXTERNAL SCHEMA"):
		if !c.external {
			return nil, true, fmt.Errorf("external schema does not exist")
		}
		f.applyExternalOptions(sql)
		return nil, true, nil
	}
	return nil, false, nil
}
