-- database: admin
SELECT role_name FROM svv_role_grants WHERE role_name = :recipient AND granted_role_name = :role;
-- params: {"recipient":"example:readers","role":"sys:dba"}
