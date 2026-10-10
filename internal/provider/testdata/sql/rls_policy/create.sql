CREATE RLS POLICY "region_filter" WITH ("region" CHARACTER VARYING(64)) USING (region = current_user);
