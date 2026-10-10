CREATE MASKING POLICY "mask_defaults" WITH ("note" character varying(256), "amount" numeric(18,0), "code" character(1)) USING (NULL::VARCHAR(256));
