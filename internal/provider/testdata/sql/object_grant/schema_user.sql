SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW GRANTS ON SCHEMA "warehouse"."serving";

GRANT USAGE ON SCHEMA "warehouse"."serving" TO "loader";

REVOKE USAGE ON SCHEMA "warehouse"."serving" FROM "loader";
