-- database: admin
CREATE ROLE "example:readers" EXTERNALID "ABC123";
-- params: {}

-- database: admin
ALTER ROLE "example:readers" OWNER TO "loader";
-- params: {}

-- database: admin
SELECT role_id, role_name, role_owner, external_id FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}
