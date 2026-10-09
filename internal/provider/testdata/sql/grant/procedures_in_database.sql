SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT role_name FROM svv_roles WHERE role_name = :role;

SHOW GRANTS FOR ROLE "readers" FROM DATABASE "analytics";

GRANT EXECUTE FOR PROCEDURES IN DATABASE "analytics" TO ROLE "readers";

REVOKE EXECUTE FOR PROCEDURES IN DATABASE "analytics" FROM ROLE "readers";
