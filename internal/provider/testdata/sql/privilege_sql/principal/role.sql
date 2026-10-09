SELECT role_name FROM svv_roles WHERE role_name = :name;

GRANT USAGE ON SCHEMA "serving" TO ROLE "readers";
