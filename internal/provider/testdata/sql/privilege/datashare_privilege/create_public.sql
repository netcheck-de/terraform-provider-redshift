-- database: admin
SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share AND BTRIM(source_database) = :database;
-- params: {"database":"admin","share":"producer"}

-- database: admin
SELECT privilege_type, admin_option FROM svv_datashare_privileges WHERE datashare_name = :share AND identity_type = 'public';
-- params: {"share":"producer"}

-- database: admin
GRANT SHARE ON DATASHARE "producer" TO PUBLIC;
-- params: {}

-- database: admin
SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share AND BTRIM(source_database) = :database;
-- params: {"database":"admin","share":"producer"}

-- database: admin
SELECT privilege_type, admin_option FROM svv_datashare_privileges WHERE datashare_name = :share AND identity_type = 'public';
-- params: {"share":"producer"}
