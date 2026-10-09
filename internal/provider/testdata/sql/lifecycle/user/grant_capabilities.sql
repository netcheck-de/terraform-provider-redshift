-- database: admin
ALTER USER "grafana" CREATEUSER;
-- params: {}

-- database: admin
ALTER USER "grafana" CREATEDB;
-- params: {}

-- database: admin
SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}
