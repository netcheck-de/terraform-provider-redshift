SELECT relschema, relname, is_rls_on, is_rls_datashare_on, rls_conjunction_type FROM svv_rls_relation WHERE datname = :database ORDER BY relschema, relname;
