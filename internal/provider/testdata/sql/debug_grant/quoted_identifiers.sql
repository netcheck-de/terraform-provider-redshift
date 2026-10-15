SHOW GRANTS ON DATABASE "Odd""Db@catalog";

GRANT DEBUG ON DATABASE "Odd""Db@catalog" TO ROLE "myidc:Audit ""Team""";

REVOKE DEBUG ON DATABASE "Odd""Db@catalog" FROM ROLE "myidc:Audit ""Team""";
