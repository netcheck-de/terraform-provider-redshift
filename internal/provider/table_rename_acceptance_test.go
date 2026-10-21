package provider

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccTableColumnRename verifies actual row preservation and automatic catalog reference updates, not just SQL.
func TestAccTableColumnRename(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t)
	schemaName := "acc_rename_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	client := testAccRolesClient(t, region, profile, workgroup)
	recorded := &recordingClient{client: client}
	connection := sqlclient.Connection{Database: database}
	configuration := func(renamed, hint bool) string {
		id, label, idHint, labelHint := "id", "label", "", ""
		if renamed {
			id, label = "event_id", "description"
			if hint {
				idHint, labelHint = `renamed_from = "id"`, `renamed_from = "label"`
			}
		}
		return fmt.Sprintf(`
provider "redshift" {
  region         = %q
  profile        = %q
  workgroup_name = %q
  database       = %q
}
resource "redshift_schema" "serving" {
  database = %q
  name     = %q
}
resource "redshift_table" "events" {
  database = redshift_schema.serving.database
  schema   = redshift_schema.serving.name
  name     = "events"
  column {
    name = %q
    %s
    type = "BIGINT"
    identity {
      seed = 1
      step = 1
    }
  }
  column {
    name = "parent_id"
    type = "BIGINT"
  }
  column {
    name = %q
    %s
    type = "VARCHAR(64)"
  }
  primary_key { columns = [%q] }
  unique { columns = [%q, %q] }
  foreign_key {
    columns = ["parent_id"]
    references {
      schema  = redshift_schema.serving.name
      table   = "events"
      columns = [%q]
    }
  }
  distribution { key = %q }
  sort_key { columns = [%q] }
}
`, region, profile, workgroup, database, database, schemaName, id, idHint, label, labelHint, id, id, label, id, id, id)
	}
	checkRows := func(id, label string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			statement := sqlclient.Stmt("SELECT").Ident(id).Kw("AS id, parent_id,").Ident(label).Kw("AS label FROM").Qualified(schemaName, "events")
			rows, err := client.Query(context.Background(), connection, statement.String(), nil)
			require.NoError(t, err)
			assert.Equal(t, []sqlclient.Row{{"id": "1", "parent_id": "1", "label": "keep me"}}, rows)
			return nil
		}
	}
	mutations := func() []string {
		var writes []string
		for _, entry := range recorded.take() {
			if !fakeRead(entry.sql) {
				writes = append(writes, entry.sql)
			}
		}
		return writes
	}
	update := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("redshift_table.events", plancheck.ResourceActionUpdate)}}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"redshift": providerserver.NewProtocol6WithError(&redshiftProvider{version: "acc", client: recorded}),
		},
		Steps: []resource.TestStep{
			{Config: configuration(false, false), Check: func(state *terraform.State) error {
				statement := sqlclient.Stmt("INSERT INTO").Qualified(schemaName, "events").Kw("(parent_id, label) VALUES (1, 'keep me')")
				_, err := client.Query(context.Background(), connection, statement.String(), nil)
				require.NoError(t, err)
				recorded.take()
				return checkRows("id", "label")(state)
			}},
			{Config: configuration(true, true), ConfigPlanChecks: update, Check: resource.ComposeTestCheckFunc(
				checkRows("event_id", "description"),
				func(*terraform.State) error {
					assert.Equal(t, []string{
						sqlclient.Stmt("ALTER TABLE").Qualified(schemaName, "events").Kw("RENAME COLUMN").Ident("id").KwIdent("TO", "event_id").String(),
						sqlclient.Stmt("ALTER TABLE").Qualified(schemaName, "events").Kw("RENAME COLUMN").Ident("label").KwIdent("TO", "description").String(),
					}, mutations(), "keys and constraints must not be rebuilt")
					return nil
				},
			)},
			{Config: configuration(true, true), PlanOnly: true},
			{Config: configuration(true, false), ConfigPlanChecks: update, Check: resource.ComposeTestCheckFunc(
				checkRows("event_id", "description"),
				func(*terraform.State) error {
					assert.Empty(t, mutations(), "hint removal must run no mutation SQL")
					return nil
				},
			)},
			{Config: configuration(true, false), PlanOnly: true},
		},
	})
}
