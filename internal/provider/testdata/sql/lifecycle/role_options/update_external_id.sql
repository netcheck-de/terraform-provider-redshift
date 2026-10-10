-- database: admin
ALTER ROLE "example:readers" EXTERNALID TO "XYZ456";
-- params: {}

-- database: admin
SELECT role_id, role_name, role_owner, external_id FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}
