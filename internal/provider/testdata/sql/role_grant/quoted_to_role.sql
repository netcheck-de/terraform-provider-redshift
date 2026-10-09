SELECT role_name FROM svv_role_grants WHERE role_name = :recipient AND granted_role_name = :role;

GRANT ROLE "example:Odd""Role" TO ROLE "Odd""Readers";

REVOKE ROLE "example:Odd""Role" FROM ROLE "Odd""Readers";
