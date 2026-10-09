-- database: admin
SELECT g.groname FROM pg_group g JOIN pg_user u ON u.usesysid = ANY(g.grolist) WHERE g.groname = :group AND u.usename = :user;
-- params: {"group":"readers","user":"grafana"}

-- database: admin
ALTER GROUP "readers" ADD USER "grafana";
-- params: {}

-- database: admin
SELECT g.groname FROM pg_group g JOIN pg_user u ON u.usesysid = ANY(g.grolist) WHERE g.groname = :group AND u.usename = :user;
-- params: {"group":"readers","user":"grafana"}
