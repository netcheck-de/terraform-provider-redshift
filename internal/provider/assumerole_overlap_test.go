package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssumeroleGrantOverlapGuard refuses to reconcile a per-command ASSUMEROLE grant whose grantee also holds ON ALL,
// fails closed when the guard fails, skips it on reads, and still lets an empty failed create be forgotten.
func TestAssumeroleGrantOverlapGuard(t *testing.T) {
	for _, selector := range []string{"DEFAULT", "arn:aws:iam::123456789012:role/reader"} {
		t.Run(selector, func(t *testing.T) {
			r := newAssumeroleGrantResource().(*privilegeResource)
			c := &privilegeCatalog{values: map[string]bool{"COPY": true}}
			guardFailure := false
			guards := 0
			r.resourceClient = testResourceClient(queryFunc(func(ctx context.Context, connection sqlclient.Connection, statement string, parameters map[string]string) ([]sqlclient.Row, error) {
				if parameters["all_arn"] != "" {
					guards++

					if guardFailure {
						return nil, errors.New("catalog unavailable")
					}

					return []sqlclient.Row{{"iam_arn": "default"}}, nil
				}

				return c.Query(ctx, connection, statement, parameters)
			}))
			data := privilegeObject(t, r, map[string]string{"iam_role_arn": selector, "grantee": "reader", "grantee_type": "ROLE"}, "COPY")
			actual := data
			_, found, err := r.read(context.Background(), &actual)
			require.NoError(t, err)
			require.True(t, found)
			assert.Zero(t, guards, "observational reads must not run mutation guards")
			require.ErrorContains(t, r.reconcile(context.Background(), data), "also holds ASSUMEROLE ON ALL")
			require.ErrorContains(t, r.reconcile(context.Background(), withPrivileges(data, nil)), "also holds ASSUMEROLE ON ALL")
			assert.Empty(t, c.writes, "an overlapping tuple must never mutate another tuple's access")

			guardFailure = true

			require.ErrorContains(t, r.reconcile(context.Background(), data), "catalog unavailable")
			clear(c.values)
			require.NoError(t, r.reconcile(context.Background(), withPrivileges(data, nil)), "an empty failed create can be forgotten without mutating ON ALL")
		})
	}
}

// TestAssumeroleGrantOverlapSQL pins the ON ALL overlap guard query for a grantee that needs quoting.
func TestAssumeroleGrantOverlapSQL(t *testing.T) {
	checkSQL(t, "assumerole_overlap", []sqlCase{{name: "guard", render: func() (string, error) {
		statement, _, err := assumeroleGrantOverlapQuery(`Odd"O'Reilly`, "ROLE").Build()
		return statement, err
	}}})
}
