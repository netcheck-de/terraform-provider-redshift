SELECT identity_name, system_privilege AS privilege_type FROM svv_system_privileges WHERE identity_type = 'role' AND identity_name = :role ORDER BY identity_name, system_privilege;
