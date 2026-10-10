CREATE OR REPLACE PROCEDURE "public"."sp_example"("min_id" IN integer, "total" OUT bigint) AS $$BEGIN total := min_id; END;$$ LANGUAGE plpgsql SECURITY DEFINER;
