SELECT role_name FROM svv_roles WHERE role_name = :name;

SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT ASSUMEROLE ON 'arn:aws:iam::123456789012:role/loader' TO ROLE "readers" FOR RESULT_OFFLOAD;

REVOKE ASSUMEROLE ON 'arn:aws:iam::123456789012:role/loader' FROM ROLE "readers" FOR RESULT_OFFLOAD;
