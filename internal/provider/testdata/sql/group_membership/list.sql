SELECT g.groname, u.usename FROM pg_group g JOIN pg_user u ON u.usesysid = ANY(g.grolist) ORDER BY g.groname, u.usename;
