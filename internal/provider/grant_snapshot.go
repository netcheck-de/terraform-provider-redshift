package provider

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// privilegeSnapshot lists current members and reads each object's strongest explicit privileges. Shared databases
// cannot be connected to, and SHOW GRANTS ON SCHEMA does not enumerate object grants or their grant options.
type privilegeSnapshot struct {
	// members returns a distinct object_name for every independently granted shared object, not every overload.
	members catalogCheck
	// filterMembers validates catalog metadata and selects members where SQL-side correlation is unsafe.
	filterMembers func([]sqlclient.Row) ([]sqlclient.Row, error)
	// query renders the qualified SHOW GRANTS for one member, without consulting local privilege catalogs.
	query func(sqlclient.Row) catalogCheck
	// grant renders per-member mutations for snapshots without a safe native bulk statement.
	grant func(sqlclient.Row) grantSpec
	// mutations is populated from the same member listing used for observation, never from configured SQL.
	mutations []grantSpec
	// optionUnion keeps partial grant options visible to reconciliation while observations report the intersection.
	optionUnion []string
}

// grantCatalogVisibilityQuery reports whether the session is a superuser, whose catalog reads are not
// privilege-filtered.
func grantCatalogVisibilityQuery() sqlclient.Query {
	return sqlclient.Select("usesuper").From("pg_user").Where("usename = current_user")
}

// privilegeRows preserves single-object reads and computes the common privilege/option set for a shared snapshot.
func (r *privilegeResource) privilegeRows(ctx context.Context, target privilegeTarget) ([]sqlclient.Row, bool, error) {
	database := r.targetDatabase(target)
	if target.snapshot == nil {
		rows, err := r.queryDatabase(ctx, database, target.query.sql, target.query.parameters)
		return rows, true, err
	}
	snapshot := target.snapshot
	snapshot.optionUnion = nil
	snapshot.mutations = nil
	members, err := r.queryDatabase(ctx, database, snapshot.members.sql, snapshot.members.parameters)
	if err == nil && snapshot.filterMembers != nil {
		members, err = snapshot.filterMembers(members)
	}
	if err != nil || len(members) == 0 {
		return nil, false, err
	}
	var common map[string]sqlclient.Row
	options := map[string]bool{}
	for _, member := range members {
		if member["object_name"] == "" {
			return nil, false, fmt.Errorf("snapshot catalog returned an empty object_name")
		}
		if snapshot.grant != nil {
			snapshot.mutations = append(snapshot.mutations, snapshot.grant(member))
		}
		query := snapshot.query(member)
		rows, err := r.queryDatabase(ctx, database, query.sql, query.parameters)
		if err != nil {
			return nil, false, err
		}
		strongest := map[string]sqlclient.Row{}
		for _, row := range rows {
			if target.filter != nil && !target.filter(row) {
				continue
			}
			name := normalizePrivilege(row["privilege_type"])
			option := row["admin_option"] == "true" || row["admin_option"] == "t" || strongest[name]["admin_option"] == "true"
			strongest[name] = maps.Clone(row)
			strongest[name]["admin_option"] = strconv.FormatBool(option)
			options[name] = options[name] || option
		}
		if common == nil {
			common = strongest
			continue
		}
		for name, held := range common {
			other, ok := strongest[name]
			if !ok {
				delete(common, name)
				continue
			}
			held["admin_option"] = strconv.FormatBool(held["admin_option"] == "true" && other["admin_option"] == "true")
		}
	}
	rows := make([]sqlclient.Row, 0, len(common))
	for _, row := range common {
		rows = append(rows, row)
	}
	for name, option := range options {
		if option {
			snapshot.optionUnion = append(snapshot.optionUnion, name)
		}
	}
	slices.Sort(snapshot.optionUnion)
	return rows, true, nil
}
