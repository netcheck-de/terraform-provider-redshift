-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}

-- database: admin
DROP ROLE "example:readers";
-- params: {}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}
