package provider

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// assumeroleGrantCommands are the commands FOR ALL stands for; one statement is issued per command, so a set of all
// four is the FOR ALL grant.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-for
var assumeroleGrantCommands = []sqlclient.Keyword{"COPY", "UNLOAD", "EXTERNAL FUNCTION", "CREATE MODEL"}

// assumeroleGrantDefaultRole is the catalog name SVV_IAM_PRIVILEGES reports for grants on the default role, and,
// as PG_GET_IAM_ROLE_BY_USER documents for "any available role", for grants ON ALL roles too.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_IAM_PRIVILEGES.html
// https://docs.aws.amazon.com/redshift/latest/dg/PG_GET_IAM_ROLE_BY_USER.html
const assumeroleGrantDefaultRole = "default-aws-iam-role"

// readAssumeroleGrantQuery reads the commands one identity may run with an IAM role.
func readAssumeroleGrantQuery(catalogRole, grantee, granteeType string) sqlclient.Query {
	return sqlclient.Select("command_type AS privilege_type").From("svv_iam_privileges").
		Where("iam_arn = :arn", sqlclient.Bind("arn", catalogRole)).
		Where("identity_name = :grantee", sqlclient.Bind("grantee", grantee)).
		Where("identity_type = LOWER(:kind)", sqlclient.Bind("kind", granteeType))
}

// assumeroleGrantRole renders the ON clause target and its catalog name: an IAM role ARN literal, the namespace
// default role, or ALL roles.
func assumeroleGrantRole(role string) (sqlclient.Statement, string, error) {
	switch role {
	case "default":
		// The default role is a keyword in SQL and has its own name in the catalog.
		return sqlclient.Kw("default"), assumeroleGrantDefaultRole, nil
	case "ALL":
		return sqlclient.Kw("ALL"), assumeroleGrantDefaultRole, nil
	}
	parsed, err := arn.Parse(role)
	if err != nil || parsed.Service != "iam" || parsed.AccountID == "" || len(parsed.Resource) < 6 || parsed.Resource[:5] != "role/" {
		return sqlclient.Statement{}, "", fmt.Errorf("iam_role_arn must be an IAM role ARN, default, or ALL")
	}
	return sqlclient.Lit(role), role, nil
}

// assumeroleGrantTarget validates the IAM role selector and renders the GRANT/REVOKE ASSUMEROLE commands. PUBLIC
// holds ASSUMEROLE ON ALL FOR ALL until a superuser revokes it once; like every tuple, deleting that one revokes what it
// owns and never grants the default back, because handing every IAM role to every user is not a safe side effect.
// https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT-usage-notes.html#r_GRANT-usage-notes-assumerole
func assumeroleGrantTarget(data types.Object) (privilegeTarget, error) {
	grantee, check, err := principal(data)
	if err != nil {
		return privilegeTarget{}, err
	}
	role := objectString(data, "iam_role_arn")
	sqlRole, catalogRole, err := assumeroleGrantRole(role)
	if err != nil {
		return privilegeTarget{}, err
	}
	query, err := newCatalogCheck(readAssumeroleGrantQuery(catalogRole, objectString(data, "grantee"), objectString(data, "grantee_type")))
	if err != nil {
		return privilegeTarget{}, err
	}
	return privilegeTarget{
		checks:  []catalogCheck{check},
		query:   query,
		allowed: assumeroleGrantCommands,
		grant: grantSpec{render: func(grant bool, command sqlclient.Keyword) string {
			if grant {
				return sqlclient.Stmt("GRANT ASSUMEROLE ON").Append(sqlRole).Kw("TO").Append(grantee).Kw("FOR", command).String()
			}
			return sqlclient.Stmt("REVOKE ASSUMEROLE ON").Append(sqlRole).Kw("FROM").Append(grantee).Kw("FOR", command).String()
		}},
	}, nil
}
