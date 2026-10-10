CREATE FUNCTION "public"."f_example"(integer, character varying(10)) RETURNS integer IMMUTABLE AS $$SELECT $1 + 1$$ LANGUAGE sql;
