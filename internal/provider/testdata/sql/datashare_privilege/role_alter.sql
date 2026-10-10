SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND share_name = :share AND BTRIM(source_database) = :database;

SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT privilege_type, admin_option FROM svv_datashare_privileges WHERE datashare_name = :share AND identity_type = :type AND identity_name = :grantee;

GRANT ALTER ON DATASHARE "producer" TO ROLE "share_admins";

REVOKE ALTER ON DATASHARE "producer" FROM ROLE "share_admins";
