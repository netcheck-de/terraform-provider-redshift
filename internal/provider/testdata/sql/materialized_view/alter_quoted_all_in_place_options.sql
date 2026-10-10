ALTER MATERIALIZED VIEW "Odd""Schema"."My""Summary" ALTER DISTSTYLE KEY DISTKEY "Label""Col";

ALTER MATERIALIZED VIEW "Odd""Schema"."My""Summary" ALTER COMPOUND SORTKEY ("Label""Col", "ID");

ALTER MATERIALIZED VIEW "Odd""Schema"."My""Summary" AUTO REFRESH YES;

ALTER TABLE "Odd""Schema"."My""Summary" OWNER TO "Owner""X";
