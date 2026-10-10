CREATE OR REPLACE FUNCTION "public"."f_example"(integer, character varying(64)) RETURNS character varying(128) IMMUTABLE AS $$SELECT $1 + 1$$ LANGUAGE sql;
