SELECT usename FROM pg_user WHERE usename = :name;

SELECT command_type AS privilege_type FROM svv_iam_privileges WHERE iam_arn = :arn AND identity_name = :grantee AND identity_type = LOWER(:kind);

GRANT ASSUMEROLE ON 'arn:aws:iam::123456789012:role/it''s\\Loader' TO "Odd""User" FOR UNLOAD;

REVOKE ASSUMEROLE ON 'arn:aws:iam::123456789012:role/it''s\\Loader' FROM "Odd""User" FOR UNLOAD;
