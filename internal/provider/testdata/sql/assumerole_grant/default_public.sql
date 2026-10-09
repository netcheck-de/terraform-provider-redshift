SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT ASSUMEROLE ON default TO PUBLIC FOR EXTERNAL FUNCTION;

REVOKE ASSUMEROLE ON default FROM PUBLIC FOR EXTERNAL FUNCTION;
