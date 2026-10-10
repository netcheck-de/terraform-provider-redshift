SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT function_name FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema AND function_name = :name AND argument_type = :arguments;

SELECT privilege_type, admin_option FROM svv_function_privileges WHERE namespace_name = :schema AND function_name = :name AND argument_types = :arguments AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT EXECUTE ON PROCEDURE "warehouse"."serving"."sp_load"(bigint, timestamp without time zone) TO "analyst";

REVOKE EXECUTE ON PROCEDURE "warehouse"."serving"."sp_load"(bigint, timestamp without time zone) FROM "analyst";
