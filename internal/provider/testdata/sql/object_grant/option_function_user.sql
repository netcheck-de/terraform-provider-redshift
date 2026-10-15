GRANT EXECUTE ON FUNCTION "warehouse"."serving"."f_score"(INTEGER) TO "analyst" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR EXECUTE ON FUNCTION "warehouse"."serving"."f_score"(INTEGER) FROM "analyst";
