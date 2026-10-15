SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT usename FROM pg_user WHERE usename = :name;

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW GRANTS ON SCHEMA "analytics"."serving" FOR "analyst";

GRANT USAGE ON SCHEMA "analytics"."serving" TO "analyst";

REVOKE USAGE ON SCHEMA "analytics"."serving" FROM "analyst";

GRANT USAGE ON SCHEMA "analytics"."serving" TO "analyst" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR USAGE ON SCHEMA "analytics"."serving" FROM "analyst";
