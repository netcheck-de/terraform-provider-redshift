-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"arn":"arn:aws:iam::123456789012:role/loader","grantee":"readers","kind":"ROLE"}

-- database: admin
GRANT ASSUMEROLE ON 'arn:aws:iam::123456789012:role/loader' TO ROLE "readers" FOR COPY;
-- params: {}

-- database: admin
SELECT role_name FROM svv_roles WHERE role_name = :name;
-- params: {"name":"readers"}

-- database: admin
SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);
-- params: {"arn":"arn:aws:iam::123456789012:role/loader","grantee":"readers","kind":"ROLE"}
