ALTER TEMPLATE "serving"."csv_load" RENAME TO "csv_import";

ALTER TEMPLATE "serving"."csv_import" DROP DELIMITER;
