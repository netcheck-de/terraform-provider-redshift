package provider

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// newAssumeroleGrantResource defines per-identity IAM-role command permissions.
func newAssumeroleGrantResource() resource.Resource {
	attributes := privilegeAttributes()
	granteeAttributes(attributes)
	attributes["iam_role_arn"] = privilegeString("IAM role ARN, or default for the namespace default role.", false)
	return &privilegeResource{name: "assumerole_grant", attributes: attributes, fields: []string{"iam_role_arn", "grantee", "grantee_type"}, prepare: func(data types.Object) (privilegeTarget, error) {
		recipient, check, err := principal(data)
		if err != nil {
			return privilegeTarget{}, err
		}
		role, catalogRole := objectString(data, "iam_role_arn"), objectString(data, "iam_role_arn")
		sqlRole := sqlclient.Literal(role)
		if role == "default" {
			sqlRole, catalogRole = "default", "default-aws-iam-role"
		} else {
			parsed, err := arn.Parse(role)
			if err != nil || parsed.Service != "iam" || parsed.AccountID == "" || len(parsed.Resource) < 6 || parsed.Resource[:5] != "role/" {
				return privilegeTarget{}, fmt.Errorf("iam_role_arn must be an IAM role ARN or default")
			}
		}
		return privilegeTarget{
			checks:  []catalogCheck{check},
			query:   catalogCheck{"SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind)", map[string]string{"arn": catalogRole, "grantee": objectString(data, "grantee"), "kind": objectString(data, "grantee_type")}},
			allowed: []string{"COPY", "UNLOAD", "EXTERNAL FUNCTION", "CREATE MODEL"},
			statement: func(verb, command string) string {
				direction := "TO"
				if verb == "REVOKE" {
					direction = "FROM"
				}
				return verb + " ASSUMEROLE ON " + sqlRole + " " + direction + " " + recipient + " FOR " + command
			},
		}, nil
	}}
}
