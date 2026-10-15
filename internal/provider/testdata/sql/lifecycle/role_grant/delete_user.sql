-- database: admin
SELECT role_name, admin_option FROM svv_user_grants WHERE user_name = :user AND role_name = :role;
-- params: {"role":"sys:monitor","user":"grafana"}

-- database: admin
REVOKE ROLE "sys:monitor" FROM "grafana";
-- params: {}

-- database: admin
SELECT role_name, admin_option FROM svv_user_grants WHERE user_name = :user AND role_name = :role;
-- params: {"role":"sys:monitor","user":"grafana"}
