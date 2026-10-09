SELECT role_name FROM svv_role_grants WHERE role_name = :recipient AND granted_role_name = :role;

GRANT ROLE "sys:dba" TO ROLE "example:readers";

REVOKE ROLE "sys:dba" FROM ROLE "example:readers";
