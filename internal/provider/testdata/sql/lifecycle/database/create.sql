-- database: admin
SELECT consumer_database FROM svv_datashares WHERE share_type = 'INBOUND' AND share_name = :share AND producer_account = :account AND producer_namespace = :namespace;
-- params: {"account":"123456789012","namespace":"11111111-2222-3333-4444-555555555555","share":"source"}

-- database: admin
CREATE DATABASE "analytics" WITH PERMISSIONS FROM DATASHARE "source" OF ACCOUNT '123456789012' NAMESPACE '11111111-2222-3333-4444-555555555555';
-- params: {}

-- database: admin
SHOW DATABASES LIKE 'analytics';
-- params: {}
