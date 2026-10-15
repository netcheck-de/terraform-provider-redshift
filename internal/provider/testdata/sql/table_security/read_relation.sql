SELECT c.relname FROM pg_class c JOIN pg_namespace n ON c.relnamespace = n.oid WHERE n.nspname = :schema AND c.relname = :relation AND c.relkind IN ('r', 'v');
