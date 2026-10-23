-- database: admin
SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"arn":"default-aws-iam-role","grantee":"public","kind":"PUBLIC"}

-- database: admin
SELECT iam_arn FROM svv_iam_privileges WHERE iam_arn = :all_arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"all_arn":"default","grantee":"public","kind":"PUBLIC"}

-- database: admin
GRANT ASSUMEROLE ON DEFAULT TO PUBLIC FOR EXTERNAL FUNCTION;
-- params: {}

-- database: admin
SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"arn":"default-aws-iam-role","grantee":"public","kind":"PUBLIC"}
