CREATE RLS POLICY "region_filter" WITH ("region" character varying(64)) AS "t" USING (t.region = current_user);
