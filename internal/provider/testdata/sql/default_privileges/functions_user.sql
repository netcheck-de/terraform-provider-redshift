SELECT usename FROM pg_user WHERE usename = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SELECT usename FROM pg_user WHERE usename = :name;

SELECT schema_name FROM svv_all_schemas WHERE database_name = :database AND schema_name = :schema;

SELECT privilege_type, admin_option FROM svv_default_privileges WHERE owner_name = :owner AND schema_name = :schema AND object_type = :object_type AND grantee_name = :grantee AND grantee_type = LOWER(:kind);

ALTER DEFAULT PRIVILEGES FOR USER "loader" IN SCHEMA "serving" GRANT EXECUTE ON FUNCTIONS TO "analyst";

ALTER DEFAULT PRIVILEGES FOR USER "loader" IN SCHEMA "serving" REVOKE EXECUTE ON FUNCTIONS FROM "analyst";
