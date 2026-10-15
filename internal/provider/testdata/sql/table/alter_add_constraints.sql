ALTER TABLE "serving"."events" ADD UNIQUE ("id", "note");

ALTER TABLE "serving"."events" ADD UNIQUE ("label");

ALTER TABLE "serving"."events" ADD FOREIGN KEY ("id") REFERENCES "serving"."accounts" ("id");
