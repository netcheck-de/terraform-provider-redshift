ALTER IDENTITY PROVIDER "oauth_standard" NAMESPACE 'entra';

ALTER IDENTITY PROVIDER "oauth_standard" PARAMETERS '{"issuer":"https://sts.windows.net/2sdfdsf-d475-420d-b5ac-667adad7c702/","client_id":"other","client_secret":"BUAH~ewrqewrqwerUUY^%tHe1oNZShoiU7","audience":["https://analysis.windows.net/powerbi/connector/AmazonRedshift"]}';

ALTER IDENTITY PROVIDER "oauth_standard" AUTO_CREATE_ROLES FALSE;

ALTER IDENTITY PROVIDER "oauth_standard" DISABLE;
