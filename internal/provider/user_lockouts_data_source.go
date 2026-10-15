package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newUserLockoutsDataSource)

// newUserLockoutsDataSource lists the lockout state of password users from SHOW USER LOCKOUT.
func newUserLockoutsDataSource() datasource.DataSource {
	return newCollectionDataSource(collectionSpec{
		name: "user_lockouts",
		description: "Lists the lockout state and failed-login counters of password users with `SHOW USER LOCKOUT`, without unlocking anyone. " +
			"Requires a superuser; federated users are not listed.",
		filters: map[string]schema.Attribute{
			"user_name": schema.StringAttribute{Optional: true, MarkdownDescription: "Only the user with exactly this name.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		},
		element: map[string]schema.Attribute{
			"user_name":             grantsString("Database user name."),
			"is_locked":             schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the user is currently locked."},
			"lock_reason":           grantsString("`automatic` after reaching the failed-login threshold, `manual` after `ALTER USER ... NOLOGIN`; null when the user is not locked."),
			"locked_at":             grantsString("Time the user was locked, as Redshift reports it; null when not reported."),
			"last_failed_at":        grantsString("Time of the most recent failed login, as Redshift reports it; null when not reported."),
			"failed_login_attempts": schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of consecutive failed logins."},
			"locked_by":             schema.Int64Attribute{Computed: true, MarkdownDescription: "User ID of the superuser who locked the user; null for an automatic lockout."},
		},
		list: func(ctx context.Context, client *resourceClient, data types.Object) ([]map[string]attr.Value, error) {
			rows, err := client.query(ctx, readUserLockoutsStatement(), nil)
			if err != nil {
				return nil, err
			}
			return userLockoutsItems(objectString(data, "user_name"), rows)
		},
	})
}
