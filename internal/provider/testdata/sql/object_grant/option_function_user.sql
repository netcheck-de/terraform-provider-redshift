GRANT EXECUTE ON FUNCTION "warehouse"."serving"."f_score"(integer) TO "analyst" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR EXECUTE ON FUNCTION "warehouse"."serving"."f_score"(integer) FROM "analyst";
