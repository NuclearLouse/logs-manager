INSERT INTO logs_manager.agent_bit_control (server_name) VALUES ('%s');
INSERT INTO logs_manager.served_application (server_name,service_name,service_tag_general,service_tags_alert,path_to_logfile,path_to_errlogfile,keywords_alert) 
VALUES 
('%[1]s', 'Captura-Rating', 'rating-all-log', '{rating-war-log,rating-err-log}', '/home/capture-rating/capture-rating.log', null, '{WAR,ERR}'),
('%[1]s', 'Postgres-Agent', 'pgagent_all_log', '{pgagent_err_log}', '/home', null, null);
INSERT INTO logs_manager.alert_email_control VALUES (false, false);
INSERT INTO logs_manager.alert_settings (id_served_app, urgent_if_message_contains, email_to) 
VALUES (1,'{WAR, ERR}','{giii.iggg2@gmail.com}'), (2,null,'{alert@oculeus.zohodesk.eu}');