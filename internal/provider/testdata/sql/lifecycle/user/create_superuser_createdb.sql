-- database: admin
CREATE USER "grafana" PASSWORD 'AdminPass123' CREATEUSER CREATEDB;
-- params: {}

-- database: admin
SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}
