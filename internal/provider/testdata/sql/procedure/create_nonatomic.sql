CREATE PROCEDURE "public"."sp_example"("min_id" IN integer, "total" OUT bigint) NONATOMIC AS $$BEGIN total := min_id * 2; END;$$ LANGUAGE plpgsql;
