ALTER TABLE "serving"."events" ADD COLUMN "odd""note" character varying(8) DEFAULT 'it''s \ ok' ENCODE ZSTD NOT NULL;
