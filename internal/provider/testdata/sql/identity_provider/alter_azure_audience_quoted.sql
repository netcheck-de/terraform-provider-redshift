ALTER IDENTITY PROVIDER "Odd""Azure" NAMESPACE 'it''s \\aad';

ALTER IDENTITY PROVIDER "Odd""Azure" PARAMETERS '{"issuer":"https://issuer.example/it''s\\\\\\"x\\"","client_id":"87f4aa26-78b7-410e-bf29-57b39929ef9a","client_secret":"it''s \\"a\\" \\\\secret","audience":["https://a.example/back\\\\slash","https://b.example/''quoted''"]}';

ALTER IDENTITY PROVIDER "Odd""Azure" AUTO_CREATE_ROLES TRUE EXCLUDE GROUPS LIKE '%_admins$';

ALTER IDENTITY PROVIDER "Odd""Azure" ENABLE;
