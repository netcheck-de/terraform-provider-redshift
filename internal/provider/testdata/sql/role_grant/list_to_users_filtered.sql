SELECT role_name, user_name, admin_option FROM svv_user_grants WHERE role_name = :role AND user_name = :user ORDER BY role_name, user_name;
