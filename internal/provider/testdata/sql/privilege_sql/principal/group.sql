SELECT groname FROM pg_group WHERE groname = :name;

GRANT USAGE ON SCHEMA "serving" TO GROUP "readers";
