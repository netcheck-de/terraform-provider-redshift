SELECT g.groname, g.grosysid, u.usename FROM pg_group g LEFT JOIN pg_user u ON u.usesysid = ANY(g.grolist) WHERE g.groname = :name ORDER BY u.usename;
