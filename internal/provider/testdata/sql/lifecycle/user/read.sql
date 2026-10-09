-- database: admin
SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}
