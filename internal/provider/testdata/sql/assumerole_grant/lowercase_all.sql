SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT ASSUMEROLE ON ALL TO ROLE "readers" FOR COPY;

REVOKE ASSUMEROLE ON ALL FROM ROLE "readers" FOR COPY;
