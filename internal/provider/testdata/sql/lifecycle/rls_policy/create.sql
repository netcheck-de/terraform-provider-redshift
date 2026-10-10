-- database: admin
CREATE RLS POLICY "region_filter" WITH ("region" character varying(64)) USING (region = current_user);
-- params: {}

-- database: admin
SELECT poldb, polname, polalias, polatts, polqual FROM svv_rls_policy WHERE poldb = :database AND polname = :name;
-- params: {"database":"admin","name":"region_filter"}
