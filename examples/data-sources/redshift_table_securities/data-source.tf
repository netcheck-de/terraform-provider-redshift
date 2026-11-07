data "redshift_table_securities" "sales" {
  database = "analytics"
  schema   = "sales"
}

output "rls_protected_relations" {
  value = [for relation in data.redshift_table_securities.sales.table_securities : relation.relation if relation.row_level_security]
}
