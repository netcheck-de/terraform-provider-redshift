-- database: admin
SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share AND BTRIM(source_database) = :database;
-- params: {"database":"admin","share":"producer"}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}

-- database: admin
SELECT privilege_type, admin_option FROM svv_datashare_privileges WHERE datashare_name = :share AND identity_type = :type AND identity_name = :grantee;
-- params: {"grantee":"example:readers","share":"producer","type":"role"}

-- database: admin
SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share AND BTRIM(source_database) = :database;
-- params: {"database":"admin","share":"producer"}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"example:readers"}

-- database: admin
SELECT privilege_type, admin_option FROM svv_datashare_privileges WHERE datashare_name = :share AND identity_type = :type AND identity_name = :grantee;
-- params: {"grantee":"example:readers","share":"producer","type":"role"}
