SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT function_name FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema AND function_name = :name AND NVL(argument_type, '') = '';

SELECT privilege_type, admin_option FROM svv_function_privileges WHERE namespace_name = :schema AND function_name = :name AND NVL(argument_types, '') = '' AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT EXECUTE ON PROCEDURE "warehouse"."serving"."sp_load"() TO PUBLIC;

REVOKE EXECUTE ON PROCEDURE "warehouse"."serving"."sp_load"() FROM PUBLIC;
