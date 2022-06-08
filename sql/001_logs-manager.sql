CREATE SCHEMA IF NOT EXISTS logs_manager;
-----------------------------------------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS logs_manager.all_logs (
    tag varchar NULL,
    "time" timestamp NULL,
    "data" jsonb NULL
);
-----------------------------------------------------------------------------------------------------------------
CREATE SEQUENCE IF NOT EXISTS logs_manager.alert_logs_record_id_seq
    INCREMENT 1
    START 1
    MINVALUE 1
    MAXVALUE 9223372036854775807
    CACHE 1;
-----------------------------------------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS logs_manager.alert_logs (
    tag varchar NULL,
    "time" timestamp NULL,
    "data" jsonb NULL,
    record_id bigint NOT NULL DEFAULT nextval('logs_manager.alert_logs_record_id_seq'::regclass),
    send_email boolean NOT NULL DEFAULT false
);
-----------------------------------------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS logs_manager.agent_bit_control (
    server_name varchar NOT NULL,
    stop_agent boolean NOT NULL DEFAULT false,
    reload_config boolean NOT NULL DEFAULT false,
    CONSTRAINT pk_agent_bit_control PRIMARY KEY (server_name)
);
-----------------------------------------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS logs_manager.alert_notification_control (
    stop_service boolean NOT NULL DEFAULT false,
    reload_config boolean NOT NULL DEFAULT false
);
-----------------------------------------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS logs_manager.served_application (
    id_served_app serial NOT NULL,
    server_name varchar NOT NULL,
    service_name varchar NOT NULL,
    service_tag_general varchar NOT NULL,
    service_tags_alert text[] NOT NULL,
    path_to_logfile varchar NOT NULL,
    path_to_errlogfile varchar NULL,
    keywords_alert text[] NULL,
    days_keeping_all_logs integer NOT NULL DEFAULT 3,
    days_keeping_alert_logs integer NOT NULL DEFAULT 7,
    CONSTRAINT pk_served_application PRIMARY KEY (id_served_app),
    CONSTRAINT uniq_served_application UNIQUE (server_name, service_name),
    CONSTRAINT fk_served_application FOREIGN KEY (server_name)
    REFERENCES logs_manager.agent_bit_control(server_name) ON DELETE CASCADE ON UPDATE CASCADE
);
-----------------------------------------------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS logs_manager.alert_settings (
    id_served_app serial NOT NULL,
    send_urgent bool NOT NULL DEFAULT true,
    urgent_if_message_contains text[] NULL,	
    email_to text[] NOT NULL DEFAULT '{alert@oculeus.zohodesk.eu}',
    bitrix_to text[],
    CONSTRAINT fk_alert_settings FOREIGN KEY (id_served_app)
    REFERENCES logs_manager.served_application(id_served_app) ON DELETE CASCADE ON UPDATE CASCADE
);
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION logs_manager.delete_old_logs()
RETURNS TABLE (
    server_name varchar,
    service_name varchar,
    clear_all_logs bigint,
    clear_err_logs bigint
)
LANGUAGE plpgsql
AS $$
DECLARE srvc RECORD;
BEGIN 
CREATE TEMP TABLE temp_clear_old_logs_data (   
    server_name varchar,
    service_name varchar,
    clear_all_logs bigint,
    clear_err_logs bigint) ON COMMIT DROP;
FOR srvc IN( 
    SELECT lms.server_name  AS server_name,
    lms.service_name  AS service_name,
    lms.service_tag_general AS tag_general,
    lms.service_tags_alert AS tag_err,
    lms.days_keeping_all_logs AS keep_all,
    lms.days_keeping_alert_logs AS keep_err
    FROM logs_manager.served_application lms
) LOOP

    DELETE FROM logs_manager.all_logs WHERE "time" < now()::date - make_interval(days => srvc.keep_all)
    AND tag = srvc.tag_general;
    GET DIAGNOSTICS clear_all_logs = ROW_COUNT;

    DELETE FROM logs_manager.alert_logs WHERE "time" < now()::date - make_interval(days => srvc.keep_err)
    AND tag = ANY (srvc.tag_err) AND send_email = TRUE ;
    GET DIAGNOSTICS clear_err_logs = ROW_COUNT;
--TODO: может переделать на возврат только id_served_app вместо имя_сервера и имя_приложения
    INSERT INTO temp_clear_old_logs_data VALUES (srvc.server_name, srvc.service_name, clear_all_logs, clear_err_logs);
END LOOP;
 RETURN QUERY
    SELECT * FROM temp_clear_old_logs_data; 
END;
$$
;
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION logs_manager.served_app_alert_settings(in_served_app_id integer)
RETURNS TABLE (
    server_name varchar,
    service_name varchar,
    send_urgent bool,
    urgent_if_message_contains text[],
    email_to text[]
)
LANGUAGE plpgsql
AS $$
BEGIN 
    RETURN QUERY
        SELECT sa.server_name,sa.service_name,als.send_urgent,als.urgent_if_message_contains, als.email_to
        FROM logs_manager.served_application sa 
        JOIN logs_manager.alert_settings als ON als.id_served_app = sa.id_served_app 
        WHERE sa.id_served_app = in_served_app_id;

END;
$$
;   
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION logs_manager.alert_logs_from_served_app(in_served_app_id integer)
RETURNS TABLE (
    tag varchar,
    "time" timestamp,
    "data" jsonb,
    record_id bigint
)
LANGUAGE plpgsql
AS $$
BEGIN 
    RETURN QUERY
        SELECT al.tag,al.time,al.data,al.record_id
        FROM logs_manager.alert_logs al 
        JOIN logs_manager.served_application sa ON al.tag = any(sa.service_tags_alert) 
        JOIN logs_manager.alert_settings als ON (sa.id_served_app = als.id_served_app)
        WHERE al.send_email=FALSE AND sa.id_served_app = in_served_app_id;
END;
$$
;

---- create above / drop below ----

DROP FUNCTION logs_manager.served_app_alert_settings(integer);
DROP FUNCTION logs_manager.alert_logs_from_served_app(integer);
DROP FUNCTION logs_manager.delete_old_logs();
DROP TABLE logs_manager.alert_settings;
DROP TABLE logs_manager.alert_email_control;
DROP TABLE logs_manager.served_application;
DROP TABLE logs_manager.agent_bit_control;
DROP TABLE IF EXISTS logs_manager.all_logs;
DROP TABLE logs_manager.alert_logs;
DROP SEQUENCE logs_manager.alert_logs_record_id_seq;
DROP SCHEMA logs_manager;
