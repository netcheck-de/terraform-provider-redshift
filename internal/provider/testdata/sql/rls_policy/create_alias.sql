CREATE RLS POLICY "region_filter" WITH ("region" CHARACTER VARYING(64)) AS "t" USING (t.region = current_user);
