CREATE MASKING POLICY "card_number_conditional_mask" WITH ("fraudulent" boolean, "pan" character varying(16)) USING (CASE WHEN fraudulent THEN REDACT_CREDIT_CARD(pan) ELSE NULL END);
