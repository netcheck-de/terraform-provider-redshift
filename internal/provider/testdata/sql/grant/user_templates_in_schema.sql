SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT usename FROM pg_user WHERE usename = :name;

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SHOW GRANTS ON SCHEMA "analytics"."serving" FOR "analyst";

GRANT USAGE FOR TEMPLATES IN SCHEMA "serving" DATABASE "analytics" TO "analyst";

REVOKE USAGE FOR TEMPLATES IN SCHEMA "serving" DATABASE "analytics" FROM "analyst";

GRANT USAGE FOR TEMPLATES IN SCHEMA "serving" DATABASE "analytics" TO "analyst" WITH GRANT OPTION;

REVOKE GRANT OPTION USAGE FOR TEMPLATES IN SCHEMA "serving" DATABASE "analytics" FROM "analyst";
