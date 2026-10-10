CREATE OR REPLACE PROCEDURE "public"."sp_example"("min_id" IN integer, "label" OUT character varying(64)) AS $$BEGIN total := min_id * 2; END;$$ LANGUAGE plpgsql SECURITY INVOKER;
