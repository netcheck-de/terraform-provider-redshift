CREATE MATERIALIZED VIEW "Odd""Schema"."My""Summary" DISTKEY ("Label""Col") SORTKEY ("Label""Col", "ID") AUTO REFRESH YES AS SELECT "Label""Col", 'it''s \new' AS note, id AS "ID" FROM "Odd""Schema".t;

ALTER TABLE "Odd""Schema"."My""Summary" OWNER TO "Owner""X";
