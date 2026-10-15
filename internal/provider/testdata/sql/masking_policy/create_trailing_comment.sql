CREATE MASKING POLICY "mask_comment" WITH ("email" CHARACTER VARYING(256)) USING ('***'::VARCHAR(256) -- constant mask
);
