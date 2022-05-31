CREATE SCHEMA IF NOT EXISTS web_backend__logs_manager;
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION web_backend__logs_manager.show_all_servers()
RETURNS TABLE (server_name varchar)
LANGUAGE plpgsql AS $$ 
BEGIN RETURN QUERY
    SELECT DISTINCT sa.server_name FROM logs_manager.served_application sa;
END;
$$;
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION web_backend__logs_manager.show_all_services(in_server_name varchar)
RETURNS TABLE (id integer, name varchar)
LANGUAGE plpgsql AS $$ 
BEGIN RETURN QUERY
    SELECT id_served_app, service_name
    FROM logs_manager.served_application 
    WHERE server_name = in_server_name;
END;
$$;
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION web_backend__logs_manager.get_logs_from_period(
        in_server_name varchar,
        in_service_id integer,
        time_from timestamp,
        time_to timestamp DEFAULT now()
    ) RETURNS TABLE(
        tag varchar,
        "timestamp" timestamp with time zone,
        "message" text
    )
LANGUAGE plpgsql AS $$ BEGIN
IF in_service_id = -1 THEN RETURN QUERY
SELECT al.tag, to_timestamp((al."data"->>'date')::double precision) AS "timestamp",
    al."data"->>'log' AS "message"
FROM logs_manager.all_logs al
JOIN logs_manager.served_application s ON s.service_tag_general = al.tag
WHERE time_from < al."time" AND al."time" < time_to AND s.server_name = in_server_name;
ELSE RETURN QUERY
SELECT s.service_name, to_timestamp((al."data"->>'date')::double precision) AS "timestamp",
    al."data"->>'log' AS "message"
FROM logs_manager.all_logs al
    JOIN logs_manager.served_application s ON s.service_tag_general = al.tag
WHERE time_from < al."time" AND al."time" < time_to AND s.id_served_app = in_service_id;
END IF;
END;
$$;
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION web_backend__logs_manager.get_errlogs_from_period(
    in_server_name varchar,
    in_service_id integer,
    time_from timestamp,
    time_to timestamp DEFAULT now()
) RETURNS TABLE(
    tag varchar,
    "timestamp" timestamp with time zone,
    "message" text
) 
LANGUAGE plpgsql AS $$ BEGIN 
IF in_service_id = -1 THEN RETURN QUERY
SELECT al.tag, to_timestamp((al."data"->>'date')::double precision) AS "timestamp",
    al."data"->>'log' AS "message"
FROM logs_manager.alert_logs al
JOIN logs_manager.served_application sa ON al.tag = ANY(sa.service_tags_alert)
WHERE time_from < al."time" AND al."time" < time_to AND sa.server_name = in_server_name;
ELSE RETURN QUERY
SELECT al.tag, to_timestamp((al."data"->>'date')::double precision) AS "timestamp",
    al."data"->>'log' AS "message"
FROM logs_manager.alert_logs al
JOIN logs_manager.served_application sa ON al.tag = ANY(sa.service_tags_alert)
WHERE time_from < al."time" AND al."time" < time_to AND sa.id_served_app = in_service_id;
END IF;
END;
$$;
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION web_backend__logs_manager.show_all_settings_alert_services(in_server_name varchar)
RETURNS TABLE (
    service_id integer,
    service_name varchar,
    tag_general varchar,
    tag_alerts text[],
    days_keeping_all_logs integer,
    days_keeping_err_logs integer,
    path_to_logfile varchar,
    path_to_errlogfile varchar,
    keywords_alert text[],
    send_immediately boolean,
    keyword_immediately text[],
    email_to text[]
)
LANGUAGE plpgsql AS $$
BEGIN RETURN QUERY
    SELECT ss.id_served_app, ss.service_name, ss.service_tag_general, ss.service_tags_alert, ss.days_keeping_all_logs,
    ss.days_keeping_alert_logs, ss.path_to_logfile, ss.path_to_errlogfile, ss.keywords_alert, 
    as2.send_urgent, as2.urgent_if_message_contains, as2.email_to
FROM logs_manager.served_application ss
JOIN logs_manager.alert_settings as2 ON as2.id_served_app = ss.id_served_app
WHERE ss.server_name = in_server_name;
END;
$$;
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION web_backend__logs_manager.set_settings_alert_service(
    in_server_name varchar,
    in_service_id integer,
    in_service_name varchar,
    in_tag_general varchar,
    in_tags_alert text[],
    in_days_keeping_all_logs integer,
    in_days_keeping_alert_logs integer,
    in_path_to_logfile varchar,
    in_path_to_errlogfile varchar,
    in_keywords_alert text[],
    in_send_immediately boolean,
    in_keywords_immediately text[],
    in_email_to text[],
    OUT status character varying,
    OUT message text
)
RETURNS RECORD
LANGUAGE plpgsql AS $$
DECLARE
    update_1 varchar := '';
    update_2 varchar := '';
BEGIN
    message := '';
    status	:= 'OK';
    UPDATE logs_manager.served_application SET service_name=in_service_name, service_tag_general=in_tag_general,
    service_tags_alert=in_tags_alert, days_keeping_all_logs=in_days_keeping_all_logs, days_keeping_alert_logs=in_days_keeping_alert_logs,
    path_to_logfile=in_path_to_logfile, path_to_errlogfile=in_path_to_errlogfile, keywords_alert=in_keywords_alert
    WHERE id_served_app=in_service_id;
    GET DIAGNOSTICS update_1 = ROW_COUNT;
	IF update_1 <> '0' THEN message := 'General service settings updated successfully !
';
	ELSE
    message = 'General service settings not updated !
';
    status := 'NOT OK';
	END IF;

    UPDATE logs_manager.alert_settings SET send_urgent=in_send_immediately,
    urgent_if_message_contains=in_keywords_immediately, email_to=in_email_to
    WHERE id_served_app=in_service_id;
    GET DIAGNOSTICS update_2 = ROW_COUNT;
    IF update_2 <> '0' THEN message :=  message||'Alert Email settings updated successfully !
';
    ELSE
    message :=  message ||'Alert Email settings not updated !
';
    status := 'NOT OK';
	END IF;

    UPDATE logs_manager.agent_bit_control SET reload_config = TRUE WHERE server_name=in_server_name;
END;
$$;
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION web_backend__logs_manager.delete_alert_service(
    in_server_name varchar,
    in_service_id integer,
    OUT status character varying,
    OUT message text)
RETURNS RECORD
AS $$
DECLARE
    update_1 varchar := '';
    update_2 varchar := '';
BEGIN
    message := '';
    status	:= 'OK';

    DELETE FROM logs_manager.served_application ss WHERE ss.id_served_app = in_service_id;
    GET DIAGNOSTICS update_1 = ROW_COUNT;
	IF update_1 <> '0' THEN message := 'Service deleted successfully !';
	ELSE
    message = 'Failed deleted service! !';
    status := 'NOT OK';
	END IF;

    UPDATE logs_manager.agent_bit_control SET reload_config = TRUE WHERE server_name=in_server_name;
END
$$
LANGUAGE plpgsql;
-----------------------------------------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION web_backend__logs_manager.add_alert_service(
    in_server_name character varying, 
    in_service_name character varying, 
    in_tag_general character varying, 
    in_tags_alert text[], 
    in_days_keeping_all_logs integer, 
    in_days_keeping_alert_logs integer, 
    in_path_to_logfile character varying, 
    in_path_to_errlogfile character varying, 
    in_keywords_alert text[], 
    in_send_immediately boolean, 
    in_keywords_immediately text[], 
    in_email_to text[], 
    OUT status character varying, 
    OUT message text)
 RETURNS record
 LANGUAGE plpgsql
AS $$
DECLARE
    update_1 varchar := '';
    update_2 varchar := '';
    update_4 varchar := '';
    srv_id integer;
BEGIN
    message := '';
    status	:= 'OK';

    INSERT INTO logs_manager.served_application (server_name, service_name, service_tag_general, service_tags_alert, 
    path_to_logfile, path_to_errlogfile, keywords_alert, days_keeping_all_logs, days_keeping_alert_logs)
    VALUES (in_server_name, in_service_name, in_tag_general, in_tags_alert, in_path_to_logfile, in_path_to_errlogfile, 
    in_keywords_alert, in_days_keeping_all_logs, in_days_keeping_alert_logs) RETURNING id_served_app INTO srv_id;

    GET DIAGNOSTICS update_1 = ROW_COUNT;
	IF update_1 <> '0' THEN message := 'Service added successfully !';
	ELSE
    message = 'Failed added service! !';
    status := 'NOT OK';
	END IF;

    INSERT INTO logs_manager.alert_settings (id_served_app, send_urgent, urgent_if_message_contains, email_to)
    VALUES (srv_id, in_send_immediately, in_keywords_immediately, in_email_to);

    GET DIAGNOSTICS update_2 = ROW_COUNT;
    IF update_2 = '0' THEN
    message = 'Failed added service! !';
    status := 'NOT OK';
	END IF;

    UPDATE logs_manager.agent_bit_control SET reload_config = TRUE WHERE server_name = in_server_name;
    GET DIAGNOSTICS update_4 = ROW_COUNT;
    IF update_4 = '0' THEN
    message = 'Failed added service! !';
    status := 'NOT OK';
	END IF;
END;
$$
;

