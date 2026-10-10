CREATE PROCEDURE "public"."sp_example"("min_id" IN integer, "total" OUT bigint) AS $$BEGIN total := min_id * 2; END;$$ LANGUAGE plpgsql SECURITY INVOKER SET datestyle TO 'ISO, MDY';
