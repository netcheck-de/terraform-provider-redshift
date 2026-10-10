SELECT role_name, admin_option FROM svv_user_grants WHERE user_name = :user AND role_name = :role;

GRANT ROLE "example:Odd""Role" TO "Odd""User" WITH ADMIN OPTION;

REVOKE ROLE "example:Odd""Role" FROM "Odd""User";
