SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share AND BTRIM(source_database) = :database;

SELECT groname FROM pg_group WHERE groname = :name;

SELECT privilege_type, admin_option FROM svv_datashare_privileges WHERE datashare_name = :share AND identity_type = :type AND identity_name = :grantee;

GRANT ALTER ON DATASHARE "producer" TO GROUP "readers";

REVOKE ALTER ON DATASHARE "producer" FROM GROUP "readers";
