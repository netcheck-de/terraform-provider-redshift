-- database: admin
GRANT ROLE "sys:monitor" TO "grafana";
-- params: {}

-- database: admin
SELECT role_name FROM svv_user_grants WHERE user_name = :user AND role_name = :role;
-- params: {"role":"sys:monitor","user":"grafana"}
