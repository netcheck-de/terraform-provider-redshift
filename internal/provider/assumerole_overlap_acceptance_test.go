package provider

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAccAssumeroleGrantARNOverlap probes only a disposable SQL role on an already-enabled ASSUMEROLE fixture.
// Catalog snapshots distinguish the ON ALL marker from implied per-ARN rows without changing PUBLIC's policy.
func TestAccAssumeroleGrantARNOverlap(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_ASSUMEROLE", "REDSHIFT_ACC_SPECTRUM_ROLE_ARN")
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithSharedConfigProfile(profile))
	require.NoError(t, err)
	client := &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: workgroup, SecretARN: os.Getenv("REDSHIFT_ACC_SECRET_ARN"), Timeout: 5 * time.Minute, Poll: time.Second}
	connection := sqlclient.Connection{Database: database}
	name := "acc_iam_overlap_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	roleARN := os.Getenv("REDSHIFT_ACC_SPECTRUM_ROLE_ARN")
	r := newAssumeroleGrantResource().(*privilegeResource)
	r.resourceClient = testResourceClient(client)
	r.database = types.StringValue(database)
	fields := map[string]string{"iam_role_arn": roleARN, "grantee": name, "grantee_type": "ROLE"}
	data := privilegeObject(t, r, fields)
	statementFor := func(selector string, grant bool, command sqlclient.Keyword) string {
		tuple := data
		lookupValue(&tuple, "iam_role_arn", types.StringValue(selector))
		target, err := r.target(tuple)
		require.NoError(t, err)
		return target.grant.statement(grant, command)
	}
	statement := func(selector string, grant bool) string { return statementFor(selector, grant, "COPY") }
	exec := func(statement string) {
		_, err := client.Query(ctx, connection, statement, nil)
		require.NoError(t, err, "%s", statement)
	}
	snapshot := func(label string) []sqlclient.Row {
		query, params, err := sqlclient.Select("iam_arn", "command_type").From("svv_iam_privileges").
			Where("identity_name = :name", sqlclient.Bind("name", name)).Where("identity_type = 'role'").OrderBy("iam_arn", "command_type").Build()
		require.NoError(t, err)
		rows, err := client.Query(ctx, connection, query, params)
		require.NoError(t, err)
		t.Logf("%s: %v", label, rows)
		return rows
	}
	exec(sqlclient.Stmt("CREATE ROLE").Ident(name).String())
	t.Cleanup(func() {
		// Repeated revokes clear both catalog representations even if an earlier assertion interrupted the probe.
		for _, selector := range []string{"ALL", "ALL", roleARN, "DEFAULT", "ALL"} {
			_, err := client.Query(ctx, connection, statementFor(selector, false, "ALL"), nil)
			assert.NoError(t, err)
		}
		_, err := client.Query(ctx, connection, sqlclient.Stmt("DROP ROLE").Ident(name).String(), nil)
		assert.NoError(t, err)
	})
	exec(statement(roleARN, true))
	assert.Equal(t, []sqlclient.Row{{"iam_arn": roleARN, "command_type": "COPY"}}, snapshot("explicit ARN grant"))
	exec(statement(roleARN, false))
	assert.Empty(t, snapshot("explicit ARN revoked"))
	exec(statement("ALL", true))
	// Depending on the namespace's associated and default roles, Redshift also lists implied per-ARN rows.
	assert.Contains(t, snapshot("ON ALL granted"), sqlclient.Row{"iam_arn": assumeroleGrantAllRoles, "command_type": "COPY"})
	require.ErrorContains(t, r.reconcile(ctx, withPrivileges(data, []string{"COPY"})), "also holds ASSUMEROLE ON ALL")
	exec(statementFor(roleARN, true, "UNLOAD"))
	rows := snapshot("explicit ARN UNLOAD beside ON ALL COPY")
	assert.Contains(t, rows, sqlclient.Row{"iam_arn": roleARN, "command_type": "COPY"})
	assert.Contains(t, rows, sqlclient.Row{"iam_arn": roleARN, "command_type": "UNLOAD"})
	observed := data
	_, found, err := r.read(ctx, &observed)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"COPY", "UNLOAD"}, knownStrings(observed.Attributes()["privileges"].(types.Set)), "read semantics still expose the catalog rows")
	require.ErrorContains(t, r.reconcile(ctx, withPrivileges(data, []string{"UNLOAD"})), "also holds ASSUMEROLE ON ALL")
	exec(statement(roleARN, true))
	snapshot("explicit ARN added while ON ALL held")
	exec(statement(roleARN, false))
	rows = snapshot("specific ARN revoked while ON ALL held")
	assert.NotContains(t, rows, sqlclient.Row{"iam_arn": roleARN, "command_type": "COPY"})
	assert.Contains(t, rows, sqlclient.Row{"iam_arn": assumeroleGrantAllRoles, "command_type": "COPY"})
	exec(statement("ALL", false))
	first := snapshot("first ON ALL revoke")
	assert.Equal(t, []sqlclient.Row{{"iam_arn": roleARN, "command_type": "UNLOAD"}}, first, "no residual ON ALL marker on this fixture")
	exec(statement("ALL", false))
	assert.Equal(t, first, snapshot("second ON ALL revoke"), "repeated revoke is idempotent")
	exec(statementFor(roleARN, false, "UNLOAD"))
	assert.Empty(t, snapshot("explicit UNLOAD revoked"))
	require.NoError(t, r.reconcile(ctx, withPrivileges(data, []string{"COPY"})), "ARN ownership becomes safe after ON ALL is revoked")
	require.NoError(t, r.reconcile(ctx, withPrivileges(data, nil)))
	exec(statementFor("ALL", true, "ALL"))
	var commands []string
	for _, row := range snapshot("ON ALL FOR ALL granted") {
		// Rows under associated and default roles are implied by the ON ALL grant; only its marker rows count.
		if row["iam_arn"] == assumeroleGrantAllRoles {
			commands = append(commands, normalizePrivilege(row["command_type"]))
		}
	}
	for _, command := range assumeroleGrantCommands {
		assert.Contains(t, commands, string(command), "FOR ALL includes each documented command; extra internal commands are logged, not managed")
	}
	exec(statementFor("ALL", false, "ALL"))
	snapshot("first ON ALL FOR ALL revoke")
	exec(statementFor("ALL", false, "ALL"))
	assert.Empty(t, snapshot("second ON ALL FOR ALL revoke"))
}
