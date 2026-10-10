resource "redshift_procedure" "purge" {
  database = "analytics"
  schema   = "reporting"
  name     = "sp_purge_events"

  argument {
    name = "keep_days"
    type = "integer"
  }

  argument {
    name = "deleted"
    mode = "OUT"
    type = "bigint"
  }

  security      = "DEFINER"
  configuration = { search_path = "reporting" }
  body          = <<-SQL
    BEGIN
      DELETE FROM events WHERE created_at < dateadd(day, -keep_days, getdate());
      GET DIAGNOSTICS deleted := ROW_COUNT;
    END;
  SQL
}
