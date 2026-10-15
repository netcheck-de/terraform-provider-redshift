SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share AND BTRIM(source_database) = :database;

SELECT privilege_type, admin_option FROM svv_datashare_privileges WHERE datashare_name = :share AND identity_type = 'public';

GRANT SHARE ON DATASHARE "producer" TO PUBLIC;

REVOKE SHARE ON DATASHARE "producer" FROM PUBLIC;
