ATTACH MASKING POLICY "card_number_conditional_mask" ON "public"."credit_cards" ("credit_card_number") USING ("is_fraud", "credit_card_number") TO "analyst" PRIORITY 100;
