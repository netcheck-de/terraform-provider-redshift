CREATE OR REPLACE FUNCTION "public"."f_example"(integer) RETURNS integer VOLATILE AS $$SELECT $1 * 2$$ LANGUAGE sql;
