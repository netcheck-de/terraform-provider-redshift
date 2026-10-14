SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW TEMPLATES FROM SCHEMA "serving" LIKE 'csv_load';

SHOW GRANTS ON TEMPLATE "admin"."serving"."csv_load";

GRANT ALTER ON TEMPLATE "admin"."serving"."csv_load" TO "loader";

REVOKE ALTER ON TEMPLATE "admin"."serving"."csv_load" FROM "loader";
