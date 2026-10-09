-- database: admin
ALTER DATASHARE "producer" ADD TABLE "serving"."table";
-- params: {}

-- database: admin
SELECT object_name FROM svv_datashare_objects WHERE share_type = 'OUTBOUND' AND share_name = :share AND object_name = :object AND object_type IN ('table', 'view', 'late binding view', 'materialized view');
-- params: {"object":"serving.table","share":"producer"}
