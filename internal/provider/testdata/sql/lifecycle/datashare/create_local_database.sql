-- database: warehouse
CREATE DATASHARE "producer" SET PUBLICACCESSIBLE false;
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT share_name, share_type, source_database, is_publicaccessible, managed_by FROM svv_datashares WHERE share_name = :name AND share_type = 'OUTBOUND';
-- params: {"name":"producer"}
