package provider

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// readUserLockoutsStatement renders SHOW USER LOCKOUT, which has no filter clause and only reads; it never unlocks.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW_USER_LOCKOUT.html
func readUserLockoutsStatement() string {
	return sqlclient.Stmt("SHOW USER LOCKOUT").String()
}

// userLockoutBool decodes is_locked, which the transports return as true/false or t/f.
func userLockoutBool(value string) (types.Bool, error) {
	switch strings.ToLower(value) {
	case "t", "true":
		return types.BoolValue(true), nil
	case "f", "false":
		return types.BoolValue(false), nil
	}

	return types.BoolNull(), fmt.Errorf("decode is_locked %q", value)
}

// userLockoutInt decodes an integer column; SQL NULL, which rows carry as an empty string, becomes null.
func userLockoutInt(column, value string) (types.Int64, error) {
	if value == "" {
		return types.Int64Null(), nil
	}

	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return types.Int64Null(), fmt.Errorf("decode %s %q: %w", column, value, err)
	}

	return types.Int64Value(number), nil
}

// userLockoutsItems converts SHOW USER LOCKOUT rows into listing elements, keeps only userName when it is set,
// because the command cannot filter, and sorts by user name so the listing does not depend on row order.
func userLockoutsItems(userName string, rows []sqlclient.Row) ([]map[string]attr.Value, error) {
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b sqlclient.Row) int { return strings.Compare(a["user_name"], b["user_name"]) })

	items := make([]map[string]attr.Value, 0, len(sorted))
	for _, row := range sorted {
		if userName != "" && row["user_name"] != userName {
			continue
		}

		locked, err := userLockoutBool(row["is_locked"])
		if err != nil {
			return nil, err
		}

		attempts, err := userLockoutInt("failed_login_attempts", row["failed_login_attempts"])
		if err != nil {
			return nil, err
		}

		lockedBy, err := userLockoutInt("locked_by", row["locked_by"])
		if err != nil {
			return nil, err
		}

		items = append(items, map[string]attr.Value{
			"user_name": types.StringValue(row["user_name"]), "is_locked": locked,
			"lock_reason": grantsNullable(row["lock_reason"]), "locked_at": grantsNullable(row["locked_at"]),
			"last_failed_at": grantsNullable(row["last_failed_at"]), "failed_login_attempts": attempts, "locked_by": lockedBy,
		})
	}

	return items, nil
}
