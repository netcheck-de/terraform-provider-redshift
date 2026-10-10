CREATE PROCEDURE "public"."sp_example"() AS $$BEGIN total := min_id * 2; END;$$ LANGUAGE plpgsql SECURITY INVOKER;
