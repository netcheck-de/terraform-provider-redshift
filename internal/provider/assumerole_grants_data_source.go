package provider

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerDataSource(newAssumeroleGrantsDataSource)

// newAssumeroleGrantsDataSource lists the IAM role commands identities may run, per role and grantee.
func newAssumeroleGrantsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name: "assumerole_grants",
		description: "Lists `ASSUMEROLE` grants from `SVV_IAM_PRIVILEGES`, one element per IAM role and grantee, without taking ownership. " +
			"The namespace default role is listed as `DEFAULT` and a grant `ON ALL` as `ALL`; the view also repeats a grantee's `ON ALL` commands under every IAM role that another grant names. " +
			"Superusers and users with the `ACCESS SYSTEM TABLE` permission see every grant; other users see only the entries they have access to.",
		filters: map[string]schema.Attribute{
			"iam_role_arn": schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants on this IAM role ARN, `DEFAULT` for the namespace default role, or `ALL` for grants on every role; `DEFAULT` and `ALL` are accepted in any case.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
			"grantee":      schema.StringAttribute{Optional: true, MarkdownDescription: "Only grants to this identity name; `public` for `PUBLIC`.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		},
		element: describedOutputs(collectionElement(newAssumeroleGrantResource, "iam_role_arn", "grantee", "grantee_type", "privileges"), map[string]string{
			"iam_role_arn": "IAM role ARN, `DEFAULT` for the namespace default IAM role, or `ALL` for a grant on every IAM role.",
			"grantee":      "Receiving identity name; `public` for `PUBLIC`.",
			"grantee_type": "`USER`, `ROLE`, `GROUP`, or `PUBLIC`.",
			"privileges":   "Commands the grantee may run with the role, with `EXFUNC` reported as `EXTERNAL FUNCTION`.",
		}),
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			var catalogRole string

			if role := objectString(data, "iam_role_arn"); role != "" {
				var err error
				if _, catalogRole, err = assumeroleGrantRole(role); err != nil {
					return nil, err
				}
			}

			rows, err := client.selectRows(ctx, client.database.ValueString(), listAssumeroleGrantsQuery(catalogRole, objectString(data, "grantee")))
			if err != nil {
				return nil, err
			}

			groups := groupPrivilegeRows(rows, func(row sqlclient.Row) []string {
				granteeType := strings.ToUpper(row["identity_type"])
				if !slices.Contains(grantGranteeTypes, granteeType) {
					return nil
				}

				return []string{assumeroleGrantSelector(row["iam_arn"]), granteeType, row["identity_name"]}
			})

			items := make([]map[string]attr.Value, 0, len(groups))
			for _, group := range groups {
				items = append(items, map[string]attr.Value{
					"iam_role_arn": types.StringValue(group.key[0]), "grantee_type": types.StringValue(group.key[1]),
					"grantee": types.StringValue(group.key[2]), "privileges": group.fold.privilegeSet(),
				})
			}

			return items, nil
		},
	})
}
