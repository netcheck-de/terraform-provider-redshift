SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT system_privilege AS privilege_type FROM svv_system_privileges WHERE identity_type = 'role' AND identity_name = :role;

GRANT ALTER DEFAULT PRIVILEGES TO ROLE "operators";

REVOKE ALTER DEFAULT PRIVILEGES FROM ROLE "operators";
