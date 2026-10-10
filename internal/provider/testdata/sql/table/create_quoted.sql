CREATE TABLE "odd""schema"."odd""table" ("odd""id" BIGINT NOT NULL, "path\name" CHARACTER VARYING(65535) DEFAULT 'O''Reilly \ Co' ENCODE ZSTD, PRIMARY KEY ("odd""id"), UNIQUE ("path\name"), FOREIGN KEY ("odd""id") REFERENCES "ref""schema"."ref""table" ("ref""id")) DISTSTYLE KEY DISTKEY ("odd""id") COMPOUND SORTKEY ("path\name", "odd""id");

ALTER TABLE "odd""schema"."odd""table" OWNER TO "odd""owner";
