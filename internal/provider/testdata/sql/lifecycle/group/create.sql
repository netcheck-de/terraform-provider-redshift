-- database: admin
CREATE GROUP "readers";
-- params: {}

-- database: admin
SELECT groname FROM pg_group WHERE groname = :name;
-- params: {"name":"readers"}
