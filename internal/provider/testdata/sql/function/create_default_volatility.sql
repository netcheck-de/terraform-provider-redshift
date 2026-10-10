CREATE FUNCTION "public"."f_example"(integer) RETURNS integer VOLATILE AS $$SELECT $1 + 1$$ LANGUAGE sql;
