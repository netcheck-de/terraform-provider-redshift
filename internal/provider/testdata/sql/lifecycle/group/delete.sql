-- database: admin
SELECT groname FROM pg_group WHERE groname = :name;
-- params: {"name":"readers"}

-- database: admin
DROP GROUP "readers";
-- params: {}

-- database: admin
SELECT groname FROM pg_group WHERE groname = :name;
-- params: {"name":"readers"}
