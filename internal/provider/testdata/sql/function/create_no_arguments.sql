CREATE FUNCTION "public"."f_example"() RETURNS integer IMMUTABLE AS $$SELECT $1 + 1$$ LANGUAGE sql;
