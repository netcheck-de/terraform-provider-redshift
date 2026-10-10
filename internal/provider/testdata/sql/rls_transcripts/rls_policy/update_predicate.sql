-- database: admin
ALTER RLS POLICY "region_filter" USING (region = 'O''Brien' OR region IS NULL);
-- params: {}

-- database: admin
SELECT poldb, polname, polalias, polatts, polqual FROM svv_rls_policy WHERE poldb = :database AND polname = :name;
-- params: {"database":"admin","name":"region_filter"}
