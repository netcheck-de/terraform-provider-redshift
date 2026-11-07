package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccCopyTemplateEscapeRoundtrip checks on Redshift that NULL AS and DELIMITER values with tabs, backslashes, and
// quotes read back from SHOW TEMPLATE as configured.
func TestAccCopyTemplateEscapeRoundtrip(t *testing.T) {
	f := newLiveValidationFixture(t)
	// Redshift rejects an actual newline in NULL AS; literal backslash+n must remain representable.
	for i, value := range []string{"\t", `\t`, `\n`, `\\t`, `\\n`, `\`, `O'Reilly\tail`} {
		data := templateTestModel("escape_"+strconv.Itoa(i), "", "", map[string]string{"NULL": value})
		data.Schema = types.StringValue(f.schema)
		statement, err := templateDefinitionStatement("CREATE TEMPLATE", data)
		require.NoError(t, err)
		_, err = f.client.Query(context.Background(), f.conn, statement, nil)
		require.NoError(t, err)
		definition := f.templateDefinition(t, data)
		assert.Equal(t, value, definition.parameters["NULL"], "configured %q must round-trip distinctly", value)
	}

	for i, value := range []string{"\t", `\t`, `\n`, `\\`} {
		data := templateTestModel("delimiter_"+strconv.Itoa(i), "", "", map[string]string{"DELIMITER": value})
		data.Schema = types.StringValue(f.schema)
		statement, err := templateDefinitionStatement("CREATE TEMPLATE", data)
		require.NoError(t, err)
		_, err = f.client.Query(context.Background(), f.conn, statement, nil)
		require.NoError(t, err)
		definition := f.templateDefinition(t, data)
		assert.Equal(t, value, definition.parameters["DELIMITER"], "configured %q must round-trip distinctly", value)
	}
}

// TestAccCopyTemplateQuotedWhitespace checks that the templates listing keeps leading spaces in live schema, template,
// and owner names.
func TestAccCopyTemplateQuotedWhitespace(t *testing.T) {
	f := newLiveValidationFixture(t)
	schema, name, owner := " "+f.schema, " leading template", " "+f.schema+"_owner"
	f.exec(t, sqlclient.Stmt("CREATE USER").Ident(owner).Kw("PASSWORD DISABLE"))
	t.Cleanup(func() { f.exec(t, sqlclient.Stmt("DROP USER").Ident(owner)) })
	f.exec(t, sqlclient.Stmt("CREATE SCHEMA").Ident(schema))
	t.Cleanup(func() { f.exec(t, sqlclient.Stmt("DROP SCHEMA").Ident(schema).Kw("CASCADE")) })
	f.exec(t, sqlclient.Stmt("CREATE TEMPLATE").Qualified(schema, name).Kw("FOR COPY AS CSV"))
	f.exec(t, sqlclient.Stmt("ALTER TEMPLATE").Qualified(schema, name).KwIdent("OWNER TO", owner))
	client := &resourceClient{client: f.client, database: types.StringValue(f.conn.Database), warehouse: warehouseBinding{field: "workgroup_name", value: types.StringValue("live")}}
	filters := types.ObjectValueMust(map[string]attr.Type{"database": types.StringType, "schema": types.StringType}, map[string]attr.Value{"database": types.StringValue(f.conn.Database), "schema": types.StringValue(schema)})
	source := newTemplatesDataSource().(*collectionDataSource)
	items, err := source.spec.list(context.Background(), client, filters)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, types.StringValue(schema), items[0]["schema"])
	assert.Equal(t, types.StringValue(name), items[0]["name"])
	assert.Equal(t, types.StringValue(owner), items[0]["owner"])
}

// TestAccCopyTemplateVisibleListing verifies the row-visibility path without granting schema USAGE.
func TestAccCopyTemplateVisibleListing(t *testing.T) {
	f := newLiveValidationFixture(t, "REDSHIFT_ACC_DIRECT")
	owner, password := f.user(t)
	f.exec(t, sqlclient.Stmt("CREATE TEMPLATE").Qualified(f.schema, "visible").Kw("FOR COPY AS SHAPEFILE SIMPLIFY AUTO 0.000001"))
	f.exec(t, sqlclient.Stmt("ALTER TEMPLATE").Qualified(f.schema, "visible").KwIdent("OWNER TO", owner))
	t.Cleanup(func() { f.exec(t, sqlclient.Stmt("DROP TEMPLATE").Qualified(f.schema, "visible")) })
	f.templateDefinition(t, templateModel{Schema: types.StringValue(f.schema), Name: types.StringValue("visible")})

	reader := f.directUser(t, owner, password)
	for _, schema := range []string{"", f.schema} {
		rows := reader.query(t, listTemplatesQuery(f.conn.Database, schema))
		assert.Len(t, rows, 1)

		if len(rows) == 1 {
			assert.Equal(t, "visible", strings.TrimSpace(rows[0]["template_name"]))
			assert.Equal(t, owner, strings.TrimSpace(rows[0]["template_owner"]))
		}
	}
}

// TestAccCopyTemplateSimplify exercises the server-printed spatial clause and documented catalog listing.
func TestAccCopyTemplateSimplify(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	name := "acc_copy_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configuration := func(argument string) string {
		return fmt.Sprintf(`
provider "redshift" {
  region = %q
  profile = %q
  workgroup_name = %q
  database = %q
}
resource "redshift_schema" "copy" {
  database = %q
  name = %q
}
resource "redshift_template" "spatial" {
  database = redshift_schema.copy.database
  schema = redshift_schema.copy.name
  name = "spatial"
  format = "SHAPEFILE"
  format_argument = %q
}
data "redshift_template" "spatial" {
  database = redshift_template.spatial.database
  schema = redshift_template.spatial.schema
  name = redshift_template.spatial.name
}
data "redshift_templates" "database" {
  database = redshift_schema.copy.database
  depends_on = [redshift_template.spatial]
}
`, region, profile, workgroup, database, database, name, argument)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())},
		Steps: []resource.TestStep{
			{Config: configuration("SIMPLIFY AUTO 0.000001"), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("redshift_template.spatial", "format_argument", "SIMPLIFY AUTO 0.000001"),
				resource.TestCheckResourceAttr("data.redshift_template.spatial", "format", "SHAPEFILE"),
			)},
			// SHOW may change the decimal/exponent spelling; the following no-change plan checks numeric equality.
			{ResourceName: "redshift_template.spatial", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"format_argument"}},
			{Config: configuration("SIMPLIFY AUTO 0.000001"), PlanOnly: true},
			{Config: configuration("SIMPLIFY 0.01"), Check: resource.TestCheckResourceAttr("redshift_template.spatial", "format_argument", "SIMPLIFY 0.01")},
			{Config: configuration("SIMPLIFY 0.01"), PlanOnly: true},
		},
	})
}
