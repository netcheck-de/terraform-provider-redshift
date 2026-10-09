-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"arn":"default-aws-iam-role","grantee":"readers","kind":"ROLE"}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"arn":"default-aws-iam-role","grantee":"readers","kind":"ROLE"}

-- database: admin
REVOKE ASSUMEROLE ON default FROM ROLE "readers" FOR COPY;
-- params: {}

-- database: admin
REVOKE ASSUMEROLE ON default FROM ROLE "readers" FOR UNLOAD;
-- params: {}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"arn":"default-aws-iam-role","grantee":"readers","kind":"ROLE"}
