REVOKE DELETE ON TABLE "analytics"."serving"."orders" FROM "analyst";

REVOKE GRANT OPTION FOR SELECT ON TABLE "analytics"."serving"."orders" FROM "analyst";

GRANT TRUNCATE ON TABLE "analytics"."serving"."orders" TO "analyst";

GRANT INSERT ON TABLE "analytics"."serving"."orders" TO "analyst" WITH GRANT OPTION;

GRANT UPDATE ON TABLE "analytics"."serving"."orders" TO "analyst" WITH GRANT OPTION;
