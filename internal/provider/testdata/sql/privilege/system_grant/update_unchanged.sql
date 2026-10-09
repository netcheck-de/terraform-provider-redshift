-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"operators"}

-- database: admin
SELECT system_privilege AS privilege_type FROM svv_system_privileges WHERE identity_type = 'role' AND identity_name = :role;
-- params: {"role":"operators"}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"operators"}

-- database: admin
SELECT system_privilege AS privilege_type FROM svv_system_privileges WHERE identity_type = 'role' AND identity_name = :role;
-- params: {"role":"operators"}
