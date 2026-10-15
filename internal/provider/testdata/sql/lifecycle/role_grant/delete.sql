-- database: admin
SELECT role_name FROM svv_role_grants WHERE role_name = :recipient AND granted_role_name = :role;
-- params: {"recipient":"example:readers","role":"sys:dba"}

-- database: admin
REVOKE ROLE "sys:dba" FROM ROLE "example:readers";
-- params: {}

-- database: admin
SELECT role_name FROM svv_role_grants WHERE role_name = :recipient AND granted_role_name = :role;
-- params: {"recipient":"example:readers","role":"sys:dba"}
