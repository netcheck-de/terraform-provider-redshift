SELECT granted_role_name, role_name FROM svv_role_grants WHERE granted_role_name = :role AND role_name = :recipient ORDER BY granted_role_name, role_name;
