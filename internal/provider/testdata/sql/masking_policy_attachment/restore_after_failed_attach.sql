ATTACH MASKING POLICY "mask_email" ON "public"."customers" ("email") USING ("email") TO ROLE "example_readers" PRIORITY 5;
