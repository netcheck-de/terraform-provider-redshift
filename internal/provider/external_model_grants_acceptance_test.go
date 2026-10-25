package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The generated inference function is never called: visibility and explicit privilege rows prove the grant matrix.
func TestAccExternalModelGrantMatrix(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_MODEL_ROLE", "REDSHIFT_ACC_DIRECT")
	f := newLiveValidationFixture(t)
	t.Logf("isolated model grant fixture: schema=%s", f.schema)
	user, password := f.user(t)
	group := f.schema + "_group"
	f.exec(t, sqlclient.Stmt("CREATE GROUP").Ident(group).Kw("WITH USER").Ident(user))
	t.Cleanup(func() {
		_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("DROP GROUP").Ident(group).String(), nil)
		assert.NoError(t, err, "clean up isolated model group")
	})
	f.exec(t, sqlclient.Stmt("GRANT USAGE ON SCHEMA").Ident(f.schema).Kw("TO").Ident(user))
	t.Cleanup(func() {
		// Dropping the model first also removes the generated function's privileges, which can prevent DROP USER.
		_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("DROP MODEL IF EXISTS").Qualified(f.schema, "grant_model").String(), nil)
		assert.NoError(t, err, "clean up isolated model")
		_, err = f.client.Query(context.Background(), f.conn, sqlclient.Stmt("REVOKE USAGE ON SCHEMA").Ident(f.schema).Kw("FROM").Ident(user).String(), nil)
		assert.NoError(t, err)
	})

	statement, err := createExternalModelStatement(externalModelModel{
		Schema: types.StringValue(f.schema), Name: types.StringValue("grant_model"), Function: types.StringValue("infer_grant_model"),
		IAMRole: types.StringValue(os.Getenv("REDSHIFT_ACC_MODEL_ROLE")), ModelID: types.StringValue("amazon.nova-lite-v1:0"),
		RequestType: types.StringValue("UNIFIED"), ResponseType: types.StringValue("SUPER"),
	})
	require.NoError(t, err)
	_, err = f.client.Query(context.Background(), f.conn, statement, nil)
	require.NoError(t, err)
	observer := f.directUser(t, user, password)
	visible := func(t *testing.T) bool {
		t.Helper()
		rows := observer.exec(t, sqlclient.Stmt("SHOW MODEL ALL"))
		models, err := parseShowModels(rows)
		require.NoError(t, err)

		for _, model := range models {
			if model.schema == f.schema && model.name == "grant_model" {
				return true
			}
		}

		return false
	}
	initial := f.query(t, sqlclient.Select("identity_name", "identity_type", "privilege_type", "admin_option").From("svv_ml_model_privileges").
		Where("namespace_name = :schema", sqlclient.Bind("schema", f.schema)).Where("model_name = :name", sqlclient.Bind("name", "grant_model")))
	initialVisible := visible(t)
	t.Logf("fresh model: explicit_rows=%v nonowner_visible=%t", initial, initialVisible)
	f.exec(t, sqlclient.Stmt("REVOKE EXECUTE ON MODEL").Qualified(f.schema, "grant_model").Kw("FROM PUBLIC"))
	afterRevokeVisible := visible(t)
	t.Logf("after initial PUBLIC revoke: nonowner_visible=%t", afterRevokeVisible)
	assert.False(t, afterRevokeVisible, "without any model grant the isolated non-owner must not see the model")
	assert.False(t, initialVisible, "new external models must not implicitly grant PUBLIC EXECUTE; otherwise the provider needs implicit-revocation handling")

	for _, row := range initial {
		assert.NotEqual(t, "public", row["identity_type"], "fresh model must not contain a PUBLIC grant")
	}

	for _, recipient := range []struct{ kind, name string }{{"USER", user}, {"GROUP", group}, {"PUBLIC", "public"}} {
		t.Run(recipient.kind, func(t *testing.T) {
			configuration := func(execute, option bool) string {
				privileges, options := "[]", "[]"
				if execute {
					privileges = `["EXECUTE"]`
				}

				if option {
					options = `["EXECUTE"]`
				}

				return fmt.Sprintf(`
provider "redshift" {
 region = %q
 profile = %q
 workgroup_name = %q
 database = %q
}
resource "redshift_object_grant" "model" {
 database_name = %q
 schema_name = %q
 object_name = "grant_model"
 object_type = "MODEL"
 grantee = %q
 grantee_type = %q
 privileges = %s
 grant_option_privileges = %s
}
data "redshift_object_grant" "model" {
 database_name = redshift_object_grant.model.database_name
 schema_name = redshift_object_grant.model.schema_name
 object_name = redshift_object_grant.model.object_name
 object_type = redshift_object_grant.model.object_type
 grantee = redshift_object_grant.model.grantee
 grantee_type = redshift_object_grant.model.grantee_type
}
`, region, profile, workgroup, database, database, f.schema, recipient.name, recipient.kind, privileges, options)
			}
			check := func(execute, option bool) resource.TestCheckFunc {
				return func(*terraform.State) error {
					rows := f.query(t, readObjectGrantModelQuery(f.schema, "grant_model", recipient.name, recipient.kind))
					t.Logf("%s execute=%t grant_option=%t catalog=%v", recipient.kind, execute, option, rows)

					if execute {
						require.Len(t, rows, 1)
						assert.Equal(t, "EXECUTE", rows[0]["privilege_type"])
						admin, err := strconv.ParseBool(rows[0]["admin_option"])
						require.NoError(t, err)
						assert.Equal(t, option, admin)
					} else {
						assert.Empty(t, rows)
					}

					assert.Equal(t, execute, visible(t), "effective visibility through %s", recipient.kind)

					if recipient.kind == "USER" && execute {
						grant := sqlclient.Stmt("GRANT EXECUTE ON MODEL").Qualified(f.schema, "grant_model").Kw("TO GROUP").Ident(group)

						_, err := observer.client.Query(context.Background(), observer.conn, grant.String(), nil)
						if option {
							require.NoError(t, err, "WITH GRANT OPTION must permit delegation by the non-owner")
							delegated := f.query(t, readObjectGrantModelQuery(f.schema, "grant_model", group, "GROUP"))
							require.Len(t, delegated, 1)
							observer.exec(t, sqlclient.Stmt("REVOKE EXECUTE ON MODEL").Qualified(f.schema, "grant_model").Kw("FROM GROUP").Ident(group))
							assert.Empty(t, f.query(t, readObjectGrantModelQuery(f.schema, "grant_model", group, "GROUP")))
						} else {
							// Redshift can return a warning instead of an error for an unauthorized GRANT.
							// The catalog, not statement success, proves whether delegation was allowed.
							delegated := f.query(t, readObjectGrantModelQuery(f.schema, "grant_model", group, "GROUP"))
							t.Logf("ordinary EXECUTE delegation: error=%v catalog=%v", err, delegated)
							assert.Empty(t, delegated, "ordinary EXECUTE must not permit delegation")

							if len(delegated) > 0 {
								f.exec(t, sqlclient.Stmt("REVOKE EXECUTE ON MODEL").Qualified(f.schema, "grant_model").Kw("FROM GROUP").Ident(group))
							}
						}
					}

					return nil
				}
			}
			stateCheck := func(execute, option bool) resource.TestCheckFunc {
				count, options := "0", "0"
				if execute {
					count = "1"
				}

				if option {
					options = "1"
				}

				return resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("redshift_object_grant.model", "privileges.#", count),
					resource.TestCheckResourceAttr("data.redshift_object_grant.model", "privileges.#", count),
					resource.TestCheckResourceAttr("redshift_object_grant.model", "grant_option_privileges.#", options),
					resource.TestCheckResourceAttr("data.redshift_object_grant.model", "grant_option_privileges.#", options), check(execute, option))
			}

			steps := []resource.TestStep{{Config: configuration(true, false), Check: stateCheck(true, false)}}
			if recipient.kind == "USER" {
				steps = append(steps,
					resource.TestStep{Config: configuration(true, true), Check: stateCheck(true, true)},
					resource.TestStep{Config: configuration(true, false), Check: stateCheck(true, false)})
			}

			steps = append(steps, resource.TestStep{Config: configuration(false, false), Check: stateCheck(false, false)}, resource.TestStep{Config: configuration(false, false), PlanOnly: true})
			resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){"redshift": providerserver.NewProtocol6WithError(New("test")())}, Steps: steps})
		})
	}
}
