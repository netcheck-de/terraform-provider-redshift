-- database: admin
CREATE DATASHARE "producer" SET PUBLICACCESSIBLE true;
-- params: {}

-- database: admin
SELECT share_name, share_type, source_database, is_publicaccessible, managed_by FROM svv_datashares WHERE share_name = :name AND share_type = 'OUTBOUND';
-- params: {"name":"producer"}
