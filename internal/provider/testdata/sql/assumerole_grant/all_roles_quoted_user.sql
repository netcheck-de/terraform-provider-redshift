SELECT usename FROM pg_user WHERE usename = :name;

SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT ASSUMEROLE ON ALL TO "Odd""User" FOR CREATE MODEL;

REVOKE ASSUMEROLE ON ALL FROM "Odd""User" FOR CREATE MODEL;
