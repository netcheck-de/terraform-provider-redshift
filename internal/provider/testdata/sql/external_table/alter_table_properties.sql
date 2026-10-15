ALTER TABLE "example_external"."events" SET TABLE PROPERTIES ('numRows' = '170000');

ALTER TABLE "example_external"."events" SET TABLE PROPERTIES ('orc.schema.resolution' = 'position');

ALTER TABLE "example_external"."events" SET TABLE PROPERTIES ('skip.header.line.count' = '2');
