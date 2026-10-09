-- database: admin
SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}

-- database: admin
DROP USER "grafana";
-- params: {}

-- database: admin
SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}
