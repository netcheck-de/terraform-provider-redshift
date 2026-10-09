SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';

SHOW GRANTS ON DATABASE "warehouse";

GRANT TEMPORARY ON DATABASE "warehouse" TO ROLE "readers";

REVOKE TEMPORARY ON DATABASE "warehouse" FROM ROLE "readers";
