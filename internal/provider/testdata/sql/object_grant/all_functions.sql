SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT DISTINCT schema_name FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema AND NVL(function_type, '') NOT ILIKE '%PROCEDURE%';

SELECT privilege_type, admin_option FROM (SELECT privilege_type, CASE WHEN MIN(opt) = 1 THEN 'true' ELSE 'false' END AS admin_option, COUNT(*) AS objects FROM (SELECT function_name, NVL(argument_types, '') AS arguments, privilege_type, MAX(CASE WHEN admin_option THEN 1 ELSE 0 END) AS opt FROM svv_function_privileges f WHERE namespace_name = :schema AND identity_name = :grantee AND identity_type = LOWER(:kind) AND privilege_type = 'EXECUTE' AND EXISTS (SELECT 1 FROM svv_redshift_functions r WHERE r.database_name = :database AND r.schema_name = f.namespace_name AND r.function_name = f.function_name AND NVL(r.argument_type, '') = NVL(f.argument_types, '') AND NVL(r.function_type, '') NOT ILIKE '%PROCEDURE%') GROUP BY function_name, NVL(argument_types, ''), privilege_type) AS per_object GROUP BY privilege_type) AS granted WHERE objects = (SELECT COUNT(*) FROM svv_redshift_functions WHERE database_name = :database AND schema_name = :schema AND NVL(function_type, '') NOT ILIKE '%PROCEDURE%');

GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA "serving" TO ROLE "readers";

REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA "serving" FROM ROLE "readers";
