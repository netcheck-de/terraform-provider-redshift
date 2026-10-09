-- database: admin
CREATE USER "grafana" PASSWORD 'InitialPass123' NOCREATEUSER NOCREATEDB;
-- params: {}

-- database: admin
SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}
