SELECT usesuper FROM pg_user WHERE usename = current_user;

SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT DISTINCT function_name AS object_name FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema ORDER BY object_name;

SHOW GRANTS ON FUNCTION "shared_db"."serving"."member";

GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA "shared_db"."serving" TO "reader" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR EXECUTE ON ALL FUNCTIONS IN SCHEMA "shared_db"."serving" FROM "reader";

REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA "shared_db"."serving" FROM "reader";
