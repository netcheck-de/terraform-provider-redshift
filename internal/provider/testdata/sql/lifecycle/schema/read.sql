-- database: admin
SELECT n.nspname AS schema_name, u.usename AS owner FROM pg_namespace n JOIN pg_user u ON n.nspowner = u.usesysid WHERE n.nspname = :name;
-- params: {"name":"serving"}
