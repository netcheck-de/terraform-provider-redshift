CREATE RLS POLICY "region_filter" WITH ("region" character varying(64)) USING (region = current_user);
