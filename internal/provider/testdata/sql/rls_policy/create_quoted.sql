CREATE RLS POLICY "Odd""Policy" WITH ("Odd""Region" CHARACTER VARYING(256), "tenant_id" INTEGER) AS "T""Alias" USING ("T""Alias"."Odd""Region" = 'O''Brien \ path' AND tenant_id > 0);
