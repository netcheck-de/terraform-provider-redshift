SELECT usesuper FROM pg_user WHERE usename = current_user;

SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_type FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'shared';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT function_name FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema AND function_name = :name AND NVL(function_type, '') NOT ILIKE '%PROCEDURE%';

SHOW GRANTS ON FUNCTION "shared_db"."serving"."routine";

SELECT function_name FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema AND function_name = :ambiguous_name AND NVL(function_type, '') ILIKE '%PROCEDURE%';

GRANT EXECUTE ON FUNCTION "shared_db"."serving"."routine" TO "reader" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR EXECUTE ON FUNCTION "shared_db"."serving"."routine" FROM "reader";

REVOKE EXECUTE ON FUNCTION "shared_db"."serving"."routine" FROM "reader";
