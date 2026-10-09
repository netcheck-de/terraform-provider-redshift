package provider

import (
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// errUserPasswordRequired rejects a rotation without a secret; the plan never carries write-only values.
var errUserPasswordRequired = errors.New("password_wo is required when password_wo_version changes")

// userAlter starts every ALTER USER statement for the user.
func userAlter(data userModel) sqlclient.Statement {
	return sqlclient.Stmt("ALTER USER").Ident(data.Name.ValueString())
}

// createUserStatement renders CREATE USER with the configuration secret, which the plan never carries.
// Both capabilities are always spelled out so creation never depends on Redshift defaults.
func createUserStatement(data userModel, secret string) (string, error) {
	statement := sqlclient.Stmt("CREATE USER").Ident(data.Name.ValueString()).KwLit("PASSWORD", secret).
		Toggle(data.Superuser.ValueBool(), "CREATEUSER", "NOCREATEUSER").
		Toggle(data.CreateDB.ValueBool(), "CREATEDB", "NOCREATEDB")
	return statement.String(), statement.Err()
}

// userAlterSteps change one option per statement. Null priors are skipped because state written before an
// attribute existed says nothing about the user, so there is nothing to change from.
var userAlterSteps = []alterStep[userModel]{
	{
		attribute:     "password_wo_version",
		value:         func(data userModel) attr.Value { return data.PasswordVersion },
		skipNullPrior: true,
		render: func(_, plan userModel) []string {
			return []string{userAlter(plan).KwLit("PASSWORD", plan.Password.ValueString()).String()}
		},
	},
	{
		attribute:     "superuser",
		value:         func(data userModel) attr.Value { return data.Superuser },
		skipNullPrior: true,
		render: func(_, plan userModel) []string {
			return []string{userAlter(plan).Toggle(plan.Superuser.ValueBool(), "CREATEUSER", "NOCREATEUSER").String()}
		},
	},
	{
		attribute:     "create_database",
		value:         func(data userModel) attr.Value { return data.CreateDB },
		skipNullPrior: true,
		render: func(_, plan userModel) []string {
			return []string{userAlter(plan).Toggle(plan.CreateDB.ValueBool(), "CREATEDB", "NOCREATEDB").String()}
		},
	},
}

// alterUserStatements renders the in-place changes from prev to plan. plan.Password must hold the
// configuration secret, because a rotation without one would silently keep the old password.
func alterUserStatements(prev, plan userModel) ([]string, error) {
	before, after := prev.PasswordVersion, plan.PasswordVersion
	rotates := !before.IsNull() && !after.IsUnknown() && !before.Equal(after)
	if rotates && knownString(plan.Password) == "" {
		return nil, errUserPasswordRequired
	}
	return alterStatements(prev, plan, userAlterSteps), nil
}

// dropUserStatement renders DROP USER.
func dropUserStatement(data userModel) string {
	return sqlclient.Stmt("DROP USER").Ident(data.Name.ValueString()).String()
}

// readUserQuery selects the capabilities of one user; pg_user never exposes the password.
func readUserQuery(data userModel) sqlclient.Query {
	return sqlclient.Select("usename", "usesuper", "usecreatedb").From("pg_user").
		Where("usename = :name", sqlclient.Bind("name", data.Name.ValueString()))
}
