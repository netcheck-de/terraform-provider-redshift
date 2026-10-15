ALTER USER "grafana" SET query_group TO 'etl';

ALTER USER "grafana" RESET statement_timeout;

ALTER USER "grafana" SET timezone TO 'Europe/Berlin';
