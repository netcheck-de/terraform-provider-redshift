SELECT role_name FROM svv_user_grants WHERE user_name = :user AND role_name = :role;

GRANT ROLE "sys:monitor" TO "grafana";

REVOKE ROLE "sys:monitor" FROM "grafana";
