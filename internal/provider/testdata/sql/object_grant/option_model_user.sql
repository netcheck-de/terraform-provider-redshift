GRANT EXECUTE ON MODEL "serving"."summarizer" TO "analyst" WITH GRANT OPTION;

REVOKE GRANT OPTION FOR EXECUTE ON MODEL "serving"."summarizer" FROM "analyst";
