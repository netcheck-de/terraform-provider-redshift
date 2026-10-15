CREATE PROCEDURE "public"."sp_example"("min_id" IN INTEGER, "total" OUT BIGINT) NONATOMIC AS $$BEGIN total := min_id * 2; END;$$ LANGUAGE PLPGSQL;
