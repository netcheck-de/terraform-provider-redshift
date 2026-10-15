-- database: admin
SELECT poldb, polname, polalias, polatts, polqual FROM svv_rls_policy WHERE poldb = :database AND polname = :name;
-- params: {"database":"admin","name":"region_filter"}

-- database: admin
DROP RLS POLICY "region_filter";
-- params: {}

-- database: admin
SELECT poldb, polname, polalias, polatts, polqual FROM svv_rls_policy WHERE poldb = :database AND polname = :name;
-- params: {"database":"admin","name":"region_filter"}
