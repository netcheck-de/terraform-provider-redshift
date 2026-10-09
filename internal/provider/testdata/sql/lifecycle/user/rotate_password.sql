-- database: admin
ALTER USER "grafana" PASSWORD 'Rotated''Pass\\456';
-- params: {}

-- database: admin
SELECT usename, usesuper, usecreatedb FROM pg_user WHERE usename = :name;
-- params: {"name":"grafana"}
