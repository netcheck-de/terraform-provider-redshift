SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT system_privilege AS privilege_type FROM svv_system_privileges WHERE identity_type = 'role' AND identity_name = :role;

GRANT CREATE USER TO ROLE "operators";

REVOKE CREATE USER FROM ROLE "operators";
