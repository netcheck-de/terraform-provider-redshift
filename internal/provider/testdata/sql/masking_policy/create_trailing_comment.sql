CREATE MASKING POLICY "mask_comment" WITH ("email" character varying(256)) USING ('***'::VARCHAR(256) -- constant mask
);
