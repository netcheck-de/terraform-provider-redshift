resource "redshift_rls_policy" "own_region" {
  database = "analytics"
  name     = "own_region"

  columns = [
    { name = "region", type = "VARCHAR(64)" },
  ]
  predicate = "region = current_user"
}

# An alias lets the predicate qualify the columns of the protected relation.
resource "redshift_rls_policy" "tenant" {
  database = "analytics"
  name     = "tenant_rows"

  columns = [
    { name = "tenant_id", type = "INTEGER" },
  ]
  alias     = "t"
  predicate = "t.tenant_id = CAST(current_setting('app.tenant_id', FALSE) AS INTEGER)"
}
