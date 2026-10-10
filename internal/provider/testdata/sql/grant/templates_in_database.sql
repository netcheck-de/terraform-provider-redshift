SELECT database_type FROM svv_redshift_databases WHERE database_name = :database;

SELECT role_name FROM svv_roles WHERE role_name = :role;

SHOW GRANTS FOR ROLE "readers" FROM DATABASE "analytics";

GRANT USAGE FOR TEMPLATES IN DATABASE "analytics" TO ROLE "readers";

REVOKE USAGE FOR TEMPLATES IN DATABASE "analytics" FROM ROLE "readers";
