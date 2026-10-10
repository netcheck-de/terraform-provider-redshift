DETACH MASKING POLICY "mask_email" ON "public"."customers" ("email") FROM ROLE "example_readers";

ATTACH MASKING POLICY "mask_email" ON "public"."customers" ("email") TO ROLE "example_readers" PRIORITY 20;
