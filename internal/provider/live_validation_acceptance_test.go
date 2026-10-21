package provider

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveValidationFixture keeps destructive probes inside a disposable schema. Cleanup uses a fresh context so a
// failed assertion or expired query cannot strand the fixture.
type liveValidationFixture struct {
	client sqlclient.Client
	conn   sqlclient.Connection
	schema string
	ctx    context.Context
}

func newLiveValidationFixture(t *testing.T, flags ...string) liveValidationFixture {
	t.Helper()
	region, profile, workgroup, database := testAccWorkgroup(t, flags...)
	f := liveValidationFixture{client: testAccRolesClient(t, region, profile, workgroup), conn: sqlclient.Connection{Database: database}, schema: "acc_live_" + strconv.FormatInt(time.Now().UnixNano(), 36), ctx: context.Background()}
	f.exec(t, sqlclient.Stmt("CREATE SCHEMA").Ident(f.schema))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, err := f.client.Query(ctx, f.conn, sqlclient.Stmt("DROP SCHEMA").Ident(f.schema).Kw("CASCADE").String(), nil)
		assert.NoError(t, err, "clean up live-validation schema")
	})
	return f
}

func (f liveValidationFixture) exec(t *testing.T, statement sqlclient.Statement) []sqlclient.Row {
	t.Helper()
	require.NoError(t, statement.Err())
	rows, err := f.client.Query(f.ctx, f.conn, statement.String(), nil)
	require.NoError(t, err, "%s", statement.String())
	return rows
}

func (f liveValidationFixture) query(t *testing.T, query sqlclient.Query) []sqlclient.Row {
	t.Helper()
	text, args, err := query.Build()
	require.NoError(t, err)
	rows, err := f.client.Query(f.ctx, f.conn, text, args)
	require.NoError(t, err, "%s", text)
	return rows
}

func (f liveValidationFixture) user(t *testing.T) (string, string) {
	t.Helper()
	name, password := f.schema+"_user", "Acc1!"+f.schema
	f.exec(t, sqlclient.Stmt("CREATE USER").Ident(name).Kw("PASSWORD").Lit(password))
	t.Cleanup(func() {
		_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("DROP USER").Ident(name).String(), nil)
		assert.NoError(t, err, "clean up live-validation user")
	})
	return name, password
}

// directUser resolves only endpoint routing through IAM; authentication is with the isolated password user.
func (f liveValidationFixture) directUser(t *testing.T, name, password string) liveValidationFixture {
	t.Helper()
	region, profile, workgroup, _ := testAccWorkgroup(t, "REDSHIFT_ACC_DIRECT")
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	iam := &redshiftconn.IAM{Workgroup: workgroup, Serverless: redshiftserverless.NewFromConfig(cfg)}
	credentials, err := iam.Resolve(context.Background(), f.conn.Database)
	require.NoError(t, err)
	credentials.Username, credentials.Password = name, password
	f.client = &redshiftconn.Client{Credentials: credentials, CACertFile: testAccCACertFile(), SSLMode: testAccSSLMode(), Timeout: time.Minute}
	return f
}

func TestAccLiveValidationNotNullAdd(t *testing.T) {
	f := newLiveValidationFixture(t)
	for _, populated := range []bool{false, true} {
		t.Run(strconv.FormatBool(populated), func(t *testing.T) {
			name := "not_null_" + strconv.FormatBool(populated)
			f.exec(t, sqlclient.Stmt("CREATE TABLE").Qualified(f.schema, name).Kw("(id INTEGER)"))
			if populated {
				f.exec(t, sqlclient.Stmt("INSERT INTO").Qualified(f.schema, name).Kw("VALUES (1)"))
			}
			_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, name).Kw("ADD COLUMN required INTEGER NOT NULL").String(), nil)
			require.Error(t, err, "a successful ADD invalidates the provider's unconditional replacement assumption")
			assert.Contains(t, strings.ToLower(err.Error()), "default")
			columns := f.query(t, readTableAttributesQuery(tableSpec{schema: f.schema, name: name}))
			require.Len(t, columns, 1)
			assert.Equal(t, "id", columns[0]["column_name"])
		})
	}
}

func TestAccLiveValidationAutoKeyDetours(t *testing.T) {
	f := newLiveValidationFixture(t)
	t.Run("distribution", func(t *testing.T) {
		f := f
		large := os.Getenv("REDSHIFT_ACC_LARGE_TABLES") == "1"
		ctx, cancel := context.WithTimeout(f.ctx, 20*time.Minute)
		defer cancel()
		f.ctx = ctx
		var expectedRows int64
		if large {
			// RAW prevents repeated payloads compressing below AUTO's small-table threshold. Distinct keys avoid skew;
			// INSERT SELECT doubling stays on compute nodes, unlike Redshift's leader-only generate_series.
			f.exec(t, sqlclient.Stmt("CREATE TABLE").Qualified(f.schema, "auto_dist").Kw("(id INTEGER ENCODE RAW, payload VARCHAR(1024) ENCODE RAW) DISTSTYLE KEY DISTKEY(id)"))
			f.exec(t, sqlclient.Stmt("INSERT INTO").Qualified(f.schema, "auto_dist").Kw("VALUES (1, REPEAT('x', 1024))"))
			expectedRows = 1
			for range 20 {
				f.exec(t, sqlclient.Stmt("INSERT INTO").Qualified(f.schema, "auto_dist").Kw("SELECT id +").Int(expectedRows).Kw(", payload FROM").Qualified(f.schema, "auto_dist"))
				expectedRows *= 2
			}
			f.exec(t, sqlclient.Stmt("ANALYZE").Qualified(f.schema, "auto_dist"))
		} else {
			f.exec(t, sqlclient.Stmt("CREATE TABLE").Qualified(f.schema, "auto_dist").Kw("(id INTEGER, payload INTEGER) DISTSTYLE KEY DISTKEY(id)"))
		}
		spec := tableSpec{schema: f.schema, name: "auto_dist"}
		if large {
			rows := f.exec(t, sqlclient.Stmt("SELECT COUNT(*) AS row_count, SUM(OCTET_LENGTH(payload)) AS payload_bytes FROM").Qualified(f.schema, "auto_dist"))
			require.Len(t, rows, 1)
			require.Equal(t, strconv.FormatInt(expectedRows, 10), rows[0]["row_count"])
			require.Equal(t, strconv.FormatInt(expectedRows*1024, 10), rows[0]["payload_bytes"])
			size := f.query(t, sqlclient.Select("size", "tbl_rows", "diststyle").From("svv_table_info").Where(`"schema" = :schema`, sqlclient.Bind("schema", f.schema)).Where(`"table" = 'auto_dist'`))
			t.Logf("large fixture schema=%s rows=%d payload_bytes=%d SVV_TABLE_INFO=%v", f.schema, expectedRows, expectedRows*1024, size)
			require.Len(t, size, 1)
			megabytes, err := strconv.ParseInt(size[0]["size"], 10, 64)
			require.NoError(t, err)
			require.GreaterOrEqual(t, megabytes, int64(1024), "RAW fixture must reach at least 1 GiB before AUTO selection")
		}
		f.exec(t, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, "auto_dist").Kw("ALTER DISTSTYLE AUTO"))
		columns := f.query(t, readTableColumnsQuery(f.conn.Database, spec))
		require.Len(t, columns, 2)
		if large {
			catalog := f.query(t, readTableQuery(spec))
			t.Logf("after AUTO: catalog=%v columns=%v", catalog, columns)
			require.Len(t, catalog, 1)
			require.Equal(t, "9", catalog[0]["diststyle"])
			require.Equal(t, "12", catalog[0]["effective_diststyle"], "large fixture must retain AUTO(KEY), not silently skip the detour")
			require.True(t, tableCatalogBool(columns[0]["distkey"]), "AUTO must retain id as its distribution key")
		}
		if !tableCatalogBool(columns[0]["distkey"]) {
			t.Skip("AUTO did not retain a distribution key; set REDSHIFT_ACC_LARGE_TABLES=1 on a disposable warehouse to load a 1 GiB RAW fixture")
		}
		f.exec(t, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, "auto_dist").Kw("ALTER DISTSTYLE EVEN"))
		f.exec(t, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, "auto_dist").Kw("DROP COLUMN id"))
		f.exec(t, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, "auto_dist").Kw("ALTER DISTSTYLE AUTO"))
		rows := f.query(t, readTableQuery(spec))
		require.Len(t, rows, 1)
		assert.Equal(t, "9", rows[0]["diststyle"])
		assert.Len(t, f.query(t, readTableAttributesQuery(spec)), 1)
		if large {
			payload := f.exec(t, sqlclient.Stmt("SELECT COUNT(*) AS row_count, SUM(OCTET_LENGTH(payload)) AS payload_bytes FROM").Qualified(f.schema, "auto_dist"))
			require.Len(t, payload, 1)
			assert.Equal(t, strconv.FormatInt(expectedRows, 10), payload[0]["row_count"])
			assert.Equal(t, strconv.FormatInt(expectedRows*1024, 10), payload[0]["payload_bytes"])
			t.Logf("EVEN/drop/return-AUTO preserved payload: %v; final catalog=%v", payload, rows)
		}
	})
	t.Run("sort", func(t *testing.T) {
		f.exec(t, sqlclient.Stmt("CREATE TABLE").Qualified(f.schema, "auto_sort").Kw("(id INTEGER, payload INTEGER) SORTKEY(id)"))
		f.exec(t, sqlclient.Stmt("INSERT INTO").Qualified(f.schema, "auto_sort").Kw("VALUES (1, 1)"))
		f.exec(t, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, "auto_sort").Kw("ALTER SORTKEY AUTO"))
		spec := tableSpec{schema: f.schema, name: "auto_sort"}
		columns := f.query(t, readTableColumnsQuery(f.conn.Database, spec))
		require.Len(t, columns, 2)
		if columns[0]["sortkey"] == "0" || columns[0]["sortkey"] == "" {
			t.Skip("AUTO did not retain a sort key; an optimizer-selected AUTO sort key table is still needed")
		}
		f.exec(t, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, "auto_sort").Kw("ALTER SORTKEY NONE"))
		f.exec(t, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, "auto_sort").Kw("DROP COLUMN id"))
		f.exec(t, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, "auto_sort").Kw("ALTER SORTKEY AUTO"))
		rows := f.query(t, readTableSortKeyQuery(spec))
		require.Len(t, rows, 1)
		assert.Contains(t, strings.ToUpper(rows[0]["sortkey1"]), "AUTO")
		assert.Len(t, f.query(t, readTableAttributesQuery(spec)), 1)
	})
	t.Run("interleaved", func(t *testing.T) {
		f.exec(t, sqlclient.Stmt("CREATE TABLE").Qualified(f.schema, "interleaved").Kw("(id INTEGER, payload INTEGER) DISTSTYLE KEY DISTKEY(id) INTERLEAVED SORTKEY(id, payload)"))
		_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("ALTER TABLE").Qualified(f.schema, "interleaved").Kw("ALTER DISTSTYLE EVEN").String(), nil)
		require.Error(t, err)
		assert.Contains(t, strings.ToLower(err.Error()), "interleaved")
	})
}

func TestAccLiveValidationNonSuperuserCatalog(t *testing.T) {
	f := newLiveValidationFixture(t, "REDSHIFT_ACC_DIRECT")
	name, password := f.user(t)
	reader := f.directUser(t, name, password)
	f.exec(t, sqlclient.Stmt("CREATE TABLE").Qualified(f.schema, "visible").Kw("(id INTEGER, payload INTEGER) DISTSTYLE KEY DISTKEY(id)"))
	f.exec(t, sqlclient.Stmt("GRANT USAGE ON SCHEMA").Ident(f.schema).KwIdent("TO", name))
	f.exec(t, sqlclient.Stmt("GRANT SELECT ON TABLE").Qualified(f.schema, "visible").KwIdent("TO", name))
	// Revoke before dropping the user: schema cleanup is registered before user cleanup and runs last.
	t.Cleanup(func() {
		f.exec(t, sqlclient.Stmt("REVOKE SELECT ON TABLE").Qualified(f.schema, "visible").KwIdent("FROM", name))
		f.exec(t, sqlclient.Stmt("REVOKE USAGE ON SCHEMA").Ident(f.schema).KwIdent("FROM", name))
	})
	identity := reader.query(t, sqlclient.Select("usename", "usesuper").From("pg_user").Where("usename = current_user"))
	require.Len(t, identity, 1)
	assert.Equal(t, name, identity[0]["usename"])
	assert.False(t, tableCatalogBool(identity[0]["usesuper"]))
	spec := tableSpec{schema: f.schema, name: "visible"}
	rows := reader.query(t, readTableQuery(spec))
	require.Len(t, rows, 1)
	assert.Equal(t, "1", rows[0]["effective_diststyle"], "PG_CLASS_INFO must expose the effective KEY style")
	columns := reader.query(t, readTableColumnsQuery(f.conn.Database, spec))
	require.Len(t, columns, 2)
	assert.True(t, tableCatalogBool(columns[0]["distkey"]), "SVV_REDSHIFT_COLUMNS must expose the granted table's distribution key")
}

func TestAccLiveValidationMaterializedViewReset(t *testing.T) {
	f := newLiveValidationFixture(t)
	f.exec(t, sqlclient.Stmt("CREATE TABLE").Qualified(f.schema, "source").Kw("(id INTEGER, payload INTEGER)"))
	f.exec(t, sqlclient.Stmt("CREATE MATERIALIZED VIEW").Qualified(f.schema, "mv").Kw("DISTSTYLE KEY DISTKEY(id) SORTKEY(id) AS SELECT id, payload FROM").Qualified(f.schema, "source"))
	before := materializedViewModel{Schema: types.StringValue(f.schema), Name: types.StringValue("mv"), Distribution: materializedViewDistributionBlock("KEY", "id"), SortKey: materializedViewSortKeyBlock("id")}
	after := before
	after.Distribution = types.ObjectNull(materializedViewDistributionTypes)
	after.SortKey = types.ObjectNull(materializedViewSortKeyTypes)
	statements, err := alterMaterializedViewStatements(before, after)
	require.NoError(t, err)
	require.Len(t, statements, 2)
	for _, statement := range statements {
		_, err = f.client.Query(context.Background(), f.conn, statement, nil)
		require.NoError(t, err, "%s", statement)
	}
	rows := f.query(t, sqlclient.Select("c.reldiststyle AS diststyle").From("pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace").Where("n.nspname = :schema", sqlclient.Bind("schema", f.schema)).Where("c.relname = 'mv'"))
	require.Len(t, rows, 1)
	assert.Equal(t, "0", rows[0]["diststyle"])
	columns := f.query(t, readTableColumnsQuery(f.conn.Database, tableSpec{schema: f.schema, name: "mv"}))
	require.Len(t, columns, 2)
	for _, column := range columns {
		assert.False(t, tableCatalogBool(column["distkey"]))
		assert.Equal(t, "0", column["sortkey"])
	}
}

func (f liveValidationFixture) templateDefinition(t *testing.T, data templateModel) copyDefinition {
	t.Helper()
	rows, err := f.client.Query(context.Background(), f.conn, showTemplateStatement(data), nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	text, err := templateDefinitionText(rows[0])
	require.NoError(t, err)
	definition, err := parseTemplateDefinition(text)
	require.NoError(t, err, "SHOW TEMPLATE: %s", text)
	return definition
}

func TestAccLiveValidationTemplateKeywords(t *testing.T) {
	f := newLiveValidationFixture(t)
	// Each keyword gets its own template: mutually exclusive compression and error options must not mask acceptance.
	for _, keyword := range copyParameterNames(false) {
		t.Run(keyword, func(t *testing.T) {
			_, parameter, err := copyParameterKeyword(keyword, false)
			require.NoError(t, err)
			values := []string{""}
			switch parameter.argument {
			case copyInteger:
				values = []string{"1"}
			case copyText:
				values = []string{"auto"}
			case copyOptionalText:
				values = []string{"", "?"}
			case copyChoice:
				values = nil
				for _, choice := range parameter.choices {
					values = append(values, string(choice))
				}
			}
			switch keyword {
			case "NULL":
				values = []string{`\N`, "O'Reilly", ""}
			case "DELIMITER":
				values = []string{"|", "\t"}
			case "QUOTE":
				values = []string{`"`, "'"}
			}
			for i, value := range values {
				data := templateModel{Database: types.StringValue(f.conn.Database), Schema: types.StringValue(f.schema), Name: types.StringValue("kw_" + strings.ToLower(keyword) + "_" + strconv.Itoa(i)), Format: types.StringNull(), FormatArgument: types.StringNull(), Parameters: types.MapValueMust(types.StringType, map[string]attr.Value{keyword: types.StringValue(value)})}
				if keyword == "QUOTE" {
					data.Format = types.StringValue("CSV")
				}
				statement, err := templateDefinitionStatement("CREATE TEMPLATE", data)
				require.NoError(t, err)
				_, err = f.client.Query(context.Background(), f.conn, statement, nil)
				require.NoError(t, err, "%s", statement)
				definition := f.templateDefinition(t, data)
				assert.Equal(t, knownString(data.Format), definition.format)
				assert.Equal(t, map[string]string{keyword: value}, definition.parameters)
			}
		})
	}
}

func TestAccLiveValidationTemplateAlterAndReplace(t *testing.T) {
	f := newLiveValidationFixture(t)
	owner, _ := f.user(t)
	data := templateModel{Database: types.StringValue(f.conn.Database), Schema: types.StringValue(f.schema), Name: types.StringValue("replace_me"), Format: types.StringValue("CSV"), FormatArgument: types.StringNull(), Parameters: types.MapValueMust(types.StringType, map[string]attr.Value{"IGNOREHEADER": types.StringValue("1")}), Owner: types.StringValue(owner)}
	statements, err := createTemplateStatements(data)
	require.NoError(t, err)
	for _, statement := range statements {
		_, err = f.client.Query(context.Background(), f.conn, statement, nil)
		require.NoError(t, err)
	}
	// Drop the template before its owner, regardless of whether the replacement preserves ownership.
	t.Cleanup(func() {
		statement, err := dropTemplateStatement(data)
		assert.NoError(t, err)
		_, err = f.client.Query(context.Background(), f.conn, statement, nil)
		assert.NoError(t, err)
	})
	f.exec(t, sqlclient.Stmt("GRANT USAGE ON TEMPLATE").Qualified(f.schema, "replace_me").Kw("TO PUBLIC"))
	beforeGrants := f.exec(t, sqlclient.Stmt("SHOW GRANTS ON TEMPLATE").Qualified(f.schema, "replace_me"))
	require.NotEmpty(t, beforeGrants)
	plan := data
	plan.Parameters = types.MapValueMust(types.StringType, map[string]attr.Value{"IGNOREHEADER": types.StringValue("2"), "TRIMBLANKS": types.StringValue("")})
	statements, err = alterTemplateStatements(data, plan)
	require.NoError(t, err)
	require.NotEmpty(t, statements)
	for _, statement := range statements {
		_, err = f.client.Query(context.Background(), f.conn, statement, nil)
		require.NoError(t, err, "%s", statement)
	}
	assert.Equal(t, map[string]string{"IGNOREHEADER": "2", "TRIMBLANKS": ""}, f.templateDefinition(t, plan).parameters)
	withoutHeader := plan
	withoutHeader.Parameters = types.MapValueMust(types.StringType, map[string]attr.Value{"TRIMBLANKS": types.StringValue("")})
	statements, err = alterTemplateStatements(plan, withoutHeader)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	_, err = f.client.Query(context.Background(), f.conn, statements[0], nil)
	require.NoError(t, err, "%s", statements[0])
	assert.Equal(t, map[string]string{"TRIMBLANKS": ""}, f.templateDefinition(t, withoutHeader).parameters)
	plan = withoutHeader
	// Run just CREATE OR REPLACE, not the provider's ownership-restoration statement: this probes server semantics.
	plan.Format = types.StringValue("JSON")
	plan.FormatArgument = types.StringValue("auto")
	statement, err := templateDefinitionStatement("CREATE OR REPLACE TEMPLATE", plan)
	require.NoError(t, err)
	_, err = f.client.Query(context.Background(), f.conn, statement, nil)
	require.NoError(t, err)
	definition := f.templateDefinition(t, plan)
	assert.Equal(t, "JSON", definition.format)
	assert.Equal(t, "auto", definition.formatArgument)
	rows, err := f.client.Query(context.Background(), f.conn, showTemplatesStatement("", f.schema, "replace_me"), nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, owner, rows[0]["template_owner"], "CREATE OR REPLACE should preserve owner")
	afterGrants := f.exec(t, sqlclient.Stmt("SHOW GRANTS ON TEMPLATE").Qualified(f.schema, "replace_me"))
	assert.ElementsMatch(t, beforeGrants, afterGrants, "CREATE OR REPLACE should preserve grants")
}

func TestAccLiveValidationShowUserDefaults(t *testing.T) {
	f := newLiveValidationFixture(t, "REDSHIFT_ACC_DIRECT")
	name, password := f.user(t)
	reader := f.directUser(t, name, password)
	show, err := readConfigurationStatement("statement_timeout")
	require.NoError(t, err)
	read := func(client sqlclient.Client) string {
		rows, queryErr := client.Query(context.Background(), f.conn, show, nil)
		require.NoError(t, queryErr)
		value, parseErr := configurationValue("statement_timeout", rows)
		require.NoError(t, parseErr)
		return value
	}
	adminDefault, userDefault := read(f.client), read(reader.client)
	f.exec(t, sqlclient.Stmt("ALTER USER").Ident(name).Kw("SET statement_timeout TO 43210"))
	// Every Query opens a fresh direct session, so this checks the persisted user default, not a session SET.
	assert.Equal(t, "43210", read(reader.client))
	assert.Equal(t, adminDefault, read(f.client), "isolated user defaults must not change the admin's session")
	f.exec(t, sqlclient.Stmt("ALTER USER").Ident(name).Kw("RESET statement_timeout"))
	assert.Equal(t, userDefault, read(reader.client))
}

func TestAccLiveValidationUserLockoutRollout(t *testing.T) {
	f := newLiveValidationFixture(t)
	_, err := f.client.Query(context.Background(), f.conn, readUserLockoutsStatement(), nil)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "account lockout security is not supported") {
		t.Skipf("SHOW USER LOCKOUT has not rolled out on this warehouse: %v", err)
	}
	require.NoError(t, err)
	name, _ := f.user(t)
	rows, err := f.client.Query(context.Background(), f.conn, readUserLockoutsStatement(), nil)
	require.NoError(t, err)
	items, err := userLockoutsItems(name, rows)
	require.NoError(t, err)
	require.Len(t, items, 1, "a newly created password user should be listed")
	assert.Equal(t, types.BoolValue(false), items[0]["is_locked"])
	assert.Equal(t, types.Int64Value(0), items[0]["failed_login_attempts"])
}

// This read-only probe needs an existing job tied to an active integration; it never starts ingestion or changes AUTO.
func TestAccLiveValidationCopyJobDatabaseScope(t *testing.T) {
	f := newLiveValidationFixture(t, "REDSHIFT_ACC_COPY_JOB_NAME", "REDSHIFT_ACC_COPY_JOB_DATABASE", "REDSHIFT_ACC_COPY_JOB_OTHER_DATABASE")
	job, ownerDB, otherDB := os.Getenv("REDSHIFT_ACC_COPY_JOB_NAME"), os.Getenv("REDSHIFT_ACC_COPY_JOB_DATABASE"), os.Getenv("REDSHIFT_ACC_COPY_JOB_OTHER_DATABASE")
	require.NotEqual(t, ownerDB, otherDB)
	query := sqlclient.Select("job_name").From("sys_copy_job").Where("job_name = :name", sqlclient.Bind("name", job))
	f.conn.Database = ownerDB
	rows := f.query(t, query)
	require.Len(t, rows, 1, "the supplied job must exist in its fixture database")
	f.conn.Database = otherDB
	assert.Empty(t, f.query(t, query), "SYS_COPY_JOB is warehouse-wide: provider catalog reads need a database filter")
	t.Run("non_owner", func(t *testing.T) {
		testAccPreCheck(t, "REDSHIFT_ACC_DIRECT")
		f.conn.Database = ownerDB
		name, password := f.user(t)
		reader := f.directUser(t, name, password)
		assert.Empty(t, reader.query(t, query), "an unprivileged non-owner should not see the fixture's COPY job")
	})
}
