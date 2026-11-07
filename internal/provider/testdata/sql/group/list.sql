SELECT g.groname, g.grosysid, u.usename FROM pg_group g LEFT JOIN pg_user u ON u.usesysid = ANY(g.grolist) ORDER BY g.groname, u.usename;
