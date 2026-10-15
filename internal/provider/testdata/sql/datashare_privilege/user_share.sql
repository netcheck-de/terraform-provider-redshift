SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share AND BTRIM(source_database) = :database;

SELECT usename FROM pg_user WHERE usename = :name;

SELECT privilege_type, admin_option FROM svv_datashare_privileges WHERE datashare_name = :share AND identity_type = :type AND identity_name = :grantee;

GRANT SHARE ON DATASHARE "producer" TO "loader";

REVOKE SHARE ON DATASHARE "producer" FROM "loader";
