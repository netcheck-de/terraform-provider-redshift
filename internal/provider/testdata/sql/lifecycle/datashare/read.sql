-- database: admin
SELECT d.share_name, d.share_type, d.source_database, d.consumer_database, d.is_publicaccessible, d.managed_by, d.share_id, u.usename AS owner, d.producer_account, d.producer_namespace, CAST(d.createdate AS VARCHAR) AS created_at FROM svv_datashares d LEFT JOIN pg_user u ON u.usesysid = d.share_owner WHERE d.share_name = :name AND d.share_type = 'OUTBOUND';
-- params: {"name":"producer"}
