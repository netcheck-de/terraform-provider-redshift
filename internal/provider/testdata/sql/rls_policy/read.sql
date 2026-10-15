SELECT poldb, polname, polalias, polatts, polqual FROM svv_rls_policy WHERE poldb = :database AND polname = :name;
