-- database: admin
ALTER USER "grafana" NOCREATEUSER;
-- params: {}

-- database: admin
ALTER USER "grafana" NOCREATEDB;
-- params: {}

-- database: admin
SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}
