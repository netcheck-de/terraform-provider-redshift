SELECT identity_name, system_privilege AS privilege_type FROM svv_system_privileges WHERE identity_type = 'role' ORDER BY identity_name, system_privilege;
