CREATE OR REPLACE FUNCTION "public"."f_example"(integer) RETURNS integer STABLE AS $$SELECT $1 + 1$$ LANGUAGE sql;
