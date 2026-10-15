SELECT groname FROM pg_group WHERE groname = :name;

SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT ASSUMEROLE ON DEFAULT TO GROUP "readers" FOR CREATE MODEL;

REVOKE ASSUMEROLE ON DEFAULT FROM GROUP "readers" FOR CREATE MODEL;
