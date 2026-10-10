package provider

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
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

// assumeroleGrantKeywords are the iam_role_arn values that are SQL keywords rather than ARNs: the namespace default
// role and every role. They are accepted in any case; an ARN is case-sensitive and kept byte-exact.
var assumeroleGrantKeywords = []sqlclient.Keyword{"DEFAULT", "ALL"}

// assumeroleGrantKeyword returns the canonical keyword a role selector spells, or "" for an ARN.
func assumeroleGrantKeyword(role string) sqlclient.Keyword {
	keyword, err := sqlclient.OneOf(role, assumeroleGrantKeywords...)
	if err != nil {
		return ""
	}
	return keyword
}

// assumeroleGrantCanonical records DEFAULT and ALL in their canonical uppercase spelling.
func assumeroleGrantCanonical(fields map[string]string) {
	if keyword := assumeroleGrantKeyword(fields["iam_role_arn"]); keyword != "" {
		fields["iam_role_arn"] = string(keyword)
	}
}

// assumeroleGrantRoleChanged replaces the grant unless only the case of DEFAULT or ALL changed.
func assumeroleGrantRoleChanged(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	before, after := assumeroleGrantKeyword(req.StateValue.ValueString()), assumeroleGrantKeyword(req.PlanValue.ValueString())
	resp.RequiresReplace = req.PlanValue.IsUnknown() || before == "" || before != after
}

// assumeroleGrantRole renders the ON clause target and its catalog name: an IAM role ARN literal, the namespace
// default role, or ALL roles.
func assumeroleGrantRole(role string) (sqlclient.Statement, string, error) {
	if keyword := assumeroleGrantKeyword(role); keyword != "" {
		// DEFAULT and ALL are keywords in SQL and share one name in the catalog.
		return sqlclient.Kw(keyword), assumeroleGrantDefaultRole, nil
	}
	parsed, err := arn.Parse(role)
	if err != nil || parsed.Service != "iam" || parsed.AccountID == "" || len(parsed.Resource) < 6 || parsed.Resource[:5] != "role/" {
		return sqlclient.Statement{}, "", fmt.Errorf("iam_role_arn must be an IAM role ARN, DEFAULT, or ALL")
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
