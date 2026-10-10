CREATE RLS POLICY "Odd""Policy" WITH ("Odd""Region" character varying(256), "tenant_id" integer) AS "T""Alias" USING ("T""Alias"."Odd""Region" = 'O''Brien \ path' AND tenant_id > 0);
