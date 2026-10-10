CREATE MASKING POLICY "mask_email" WITH ("email" character varying(256)) USING ('***'::VARCHAR(256));
