SELECT usesuper FROM pg_user WHERE usename = current_user;

SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT DISTINCT function_name AS object_name, function_type FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema ORDER BY object_name, function_type;

SHOW GRANTS ON FUNCTION "shared_db"."serving"."member";

GRANT EXECUTE ON FUNCTION "shared_db"."serving"."member" TO "reader" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR EXECUTE ON FUNCTION "shared_db"."serving"."member" FROM "reader";

REVOKE EXECUTE ON FUNCTION "shared_db"."serving"."member" FROM "reader";
