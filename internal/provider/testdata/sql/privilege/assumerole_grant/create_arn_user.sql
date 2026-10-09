-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"analyst"}

-- database: admin
SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"arn":"arn:aws:iam::123456789012:role/loader","grantee":"analyst","kind":"USER"}

-- database: admin
GRANT ASSUMEROLE ON 'arn:aws:iam::123456789012:role/loader' TO "analyst" FOR UNLOAD;
-- params: {}

-- database: admin
SELECT usename FROM pg_user WHERE usename = :name;
-- params: {"name":"analyst"}

-- database: admin
SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"arn":"arn:aws:iam::123456789012:role/loader","grantee":"analyst","kind":"USER"}
