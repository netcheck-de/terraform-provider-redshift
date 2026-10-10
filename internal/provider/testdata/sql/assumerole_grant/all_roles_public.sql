SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT ASSUMEROLE ON ALL TO PUBLIC FOR COPY;

REVOKE ASSUMEROLE ON ALL FROM PUBLIC FOR COPY;
