SELECT g.groname FROM pg_group g JOIN pg_user u ON u.usesysid = ANY(g.grolist) WHERE g.groname = :group AND u.usename = :user;
