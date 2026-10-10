-- database: warehouse
CREATE RLS POLICY "region_filter" WITH ("region" CHARACTER VARYING(64)) USING (region = current_user);
-- params: {}

-- database: admin
SELECT database_name FROM svv_redshift_databases WHERE database_name = :database AND database_type = 'local';
-- params: {"database":"warehouse"}

-- database: warehouse
SELECT poldb, polname, polalias, polatts, polqual FROM svv_rls_policy WHERE poldb = :database AND polname = :name;
-- params: {"database":"warehouse","name":"region_filter"}
