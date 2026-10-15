SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT model_name FROM svv_ml_model_info WHERE database_name = :database AND schema_name = :schema AND model_name = :name;

SELECT privilege_type, admin_option FROM svv_ml_model_privileges WHERE namespace_name = :schema AND model_name = :name AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT EXECUTE ON MODEL "serving"."summarizer" TO ROLE "readers";

REVOKE EXECUTE ON MODEL "serving"."summarizer" FROM ROLE "readers";
