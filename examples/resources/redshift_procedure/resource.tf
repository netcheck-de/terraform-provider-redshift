resource "redshift_procedure" "purge" {
  database = "analytics"
  schema   = "reporting"
  name     = "sp_purge_events"

  argument {
    name = "keep_days"
    type = "INTEGER"
  }

  argument {
    name = "deleted"
    mode = "OUT"
    type = "BIGINT"
  }

  security      = "DEFINER"
  configuration = { search_path = "reporting" }
  body          = <<-SQL
    BEGIN
      DELETE FROM events WHERE created_at < DATEADD(DAY, -keep_days, GETDATE());
      GET DIAGNOSTICS deleted := ROW_COUNT;
    END;
  SQL
}
