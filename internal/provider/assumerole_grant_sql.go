package provider

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// readAssumeroleGrantQuery reads the commands one identity may run with an IAM role.
func readAssumeroleGrantQuery(catalogRole, grantee, granteeType string) sqlclient.Query {
	return sqlclient.Select("command_type AS privilege_type").From("svv_iam_privileges").
		Where("iam_arn = :arn", sqlclient.Bind("arn", catalogRole)).
		Where("identity_name = :grantee", sqlclient.Bind("grantee", grantee)).
		Where("identity_type = LOWER(:kind)", sqlclient.Bind("kind", granteeType))
}

// assumeroleGrantTarget validates the IAM role selector and renders the GRANT/REVOKE ASSUMEROLE commands.
func assumeroleGrantTarget(data types.Object) (privilegeTarget, error) {
	grantee, check, err := principal(data)
	if err != nil {
		return privilegeTarget{}, err
	}
	role, catalogRole := objectString(data, "iam_role_arn"), objectString(data, "iam_role_arn")
	sqlRole := sqlclient.Lit(role)
	if role == "default" {
		// The default role is a keyword in SQL and has its own name in the catalog.
		sqlRole, catalogRole = sqlclient.Kw("default"), "default-aws-iam-role"
	} else {
		parsed, err := arn.Parse(role)
		if err != nil || parsed.Service != "iam" || parsed.AccountID == "" || len(parsed.Resource) < 6 || parsed.Resource[:5] != "role/" {
			return privilegeTarget{}, fmt.Errorf("iam_role_arn must be an IAM role ARN or default")
		}
	}
	query, err := newCatalogCheck(readAssumeroleGrantQuery(catalogRole, objectString(data, "grantee"), objectString(data, "grantee_type")))
	if err != nil {
		return privilegeTarget{}, err
	}
	return privilegeTarget{
		checks:  []catalogCheck{check},
		query:   query,
		allowed: []sqlclient.Keyword{"COPY", "UNLOAD", "EXTERNAL FUNCTION", "CREATE MODEL"},
		grant: grantSpec{render: func(grant bool, command sqlclient.Keyword) string {
			if grant {
				return sqlclient.Stmt("GRANT ASSUMEROLE ON").Append(sqlRole).Kw("TO").Append(grantee).Kw("FOR", command).String()
			}
			return sqlclient.Stmt("REVOKE ASSUMEROLE ON").Append(sqlRole).Kw("FROM").Append(grantee).Kw("FOR", command).String()
		}},
	}, nil
}
