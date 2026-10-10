CREATE MASKING POLICY "Odd""Policy" WITH ("Odd""Column" CHARACTER VARYING(64)) USING (CASE WHEN "Odd""Column" LIKE 'it''s \%' THEN 'C:\masked' ELSE "Odd""Column" END);
