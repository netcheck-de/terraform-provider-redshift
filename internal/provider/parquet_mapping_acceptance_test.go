package provider

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// REDSHIFT_ACC_PARQUET_LOCATION must contain exactly one Parquet row with explicit int32 fields
// first=11, second=22, nullable=NULL (including an int32 type for the all-null field).
// For example, upload pyarrow.table({"first": pa.array([11], type=pa.int32()),
// "second": pa.array([22], type=pa.int32()), "nullable": pa.array([None], type=pa.int32())}).
// The caller owns that S3 fixture; this test only creates and drops uniquely named SQL/Glue objects.
func TestAccSpectrumParquetColumnMapping(t *testing.T) {
	region, profile, workgroup, database := testAccWorkgroup(t, "REDSHIFT_ACC_SPECTRUM_ROLE_ARN", "REDSHIFT_ACC_SPECTRUM_GLUE_DATABASE", "REDSHIFT_ACC_PARQUET_LOCATION")
	f := liveValidationFixture{
		client: testAccRolesClient(t, region, profile, workgroup),
		conn:   sqlclient.Connection{Database: database},
		schema: "acc_parquet_" + strconv.FormatInt(time.Now().UnixNano(), 36),
	}
	version := f.exec(t, sqlclient.Stmt("SELECT version() AS version"))
	t.Logf("engine: %v; fixture: %s", version, os.Getenv("REDSHIFT_ACC_PARQUET_LOCATION"))
	f.exec(t, sqlclient.Stmt("CREATE EXTERNAL SCHEMA").Ident(f.schema).
		Kw("FROM DATA CATALOG DATABASE").Lit(os.Getenv("REDSHIFT_ACC_SPECTRUM_GLUE_DATABASE")).
		Kw("IAM_ROLE").Lit(os.Getenv("REDSHIFT_ACC_SPECTRUM_ROLE_ARN")))
	t.Cleanup(func() {
		_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("DROP SCHEMA IF EXISTS").Ident(f.schema).String(), nil)
		assert.NoError(t, err, "clean up Parquet external schema without deleting the shared Glue database")
	})

	for _, scenario := range []struct {
		name, first, second, nullable, wantFirst, wantSecond string
		property, value                                      string
	}{
		{"ordered", "first", "second", "nullable", "11", "22", "", ""},
		{"swapped", "second", "first", "nullable", "22", "11", "", ""},
		{"renamed", "renamed_first", "renamed_second", "renamed_nullable", "", "", "", ""},
		{"athena_index_access", "second", "first", "nullable", "22", "11", "parquet.column.index.access", "true"},
		{"parquet_resolution", "second", "first", "nullable", "22", "11", "parquet.schema.resolution", "position"},
		{"orc_resolution", "second", "first", "nullable", "22", "11", "orc.schema.resolution", "position"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Glue is shared, so table names must be unique even across different external schemas.
			table := f.schema + "_" + scenario.name

			t.Cleanup(func() {
				_, err := f.client.Query(context.Background(), f.conn, sqlclient.Stmt("DROP TABLE IF EXISTS").Qualified(f.schema, table).String(), nil)
				assert.NoError(t, err, "clean up Parquet Glue table")
			})

			create := sqlclient.Stmt("CREATE EXTERNAL TABLE").Qualified(f.schema, table).
				Kw("(").Ident(scenario.first).Kw("INTEGER,").Ident(scenario.second).Kw("INTEGER,").Ident(scenario.nullable).Kw("INTEGER)").
				Kw("STORED AS PARQUET LOCATION").Lit(strings.TrimRight(os.Getenv("REDSHIFT_ACC_PARQUET_LOCATION"), "/") + "/")
			if scenario.property != "" {
				create = create.Kw("TABLE PROPERTIES (").Lit(scenario.property).Kw("=").Lit(scenario.value).Kw(")")
			}

			f.exec(t, create)
			rows := f.exec(t, sqlclient.Stmt("SELECT").Ident(scenario.first).Kw("AS first_value,").Ident(scenario.second).
				Kw("AS second_value,").Ident(scenario.first).Kw("IS NULL AS first_null,").Ident(scenario.second).
				Kw("IS NULL AS second_null,").Ident(scenario.nullable).Kw("IS NULL AS null_value FROM").Qualified(f.schema, table))
			t.Logf("%s: %v", scenario.name, rows)
			require.Len(t, rows, 1)
			assert.Equal(t, scenario.wantFirst, rows[0]["first_value"], "Parquet resolves fields by name, not table column position")
			assert.Equal(t, scenario.wantSecond, rows[0]["second_value"], "Parquet resolves fields by name, not table column position")
			assert.Equal(t, scenario.name == "renamed", tableCatalogBool(rows[0]["first_null"]), "absent names produce NULL, not an empty value")
			assert.Equal(t, scenario.name == "renamed", tableCatalogBool(rows[0]["second_null"]), "absent names produce NULL, not an empty value")
			assert.True(t, tableCatalogBool(rows[0]["null_value"]), "the explicitly typed Parquet NULL must remain NULL")
		})
	}
}
