-- database: admin
ALTER DATASHARE "producer" ADD SCHEMA "serving";
-- params: {}

-- database: admin
ALTER DATASHARE "producer" SET INCLUDENEW TRUE FOR SCHEMA "serving";
-- params: {}

-- database: admin
SELECT object_name, include_new FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_name = :schema AND object_type IN ('schema', 'schemas');
-- params: {"schema":"serving","share":"producer"}
