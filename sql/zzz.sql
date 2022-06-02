do $$
declare srvr_name varchar := '';
begin
INSERT INTO logs_manager.agent_bit_control (server_name) VALUES (srvr_name);

INSERT INTO logs_manager.served_application (server_name,service_name,service_tag_general,service_tags_alert,path_to_logfile,path_to_errlogfile,keywords_alert) 
VALUES 
(srvr_name, 'Captura-Rating', 'rating-all-log', '{rating-war-log,rating-err-log}', '/home/capture-rating/capture-rating.log', null, '{WAR,ERR}'),
(srvr_name, 'Postgres-Agent', 'pgagent_all_log', '{pgagent_err_log}', '/home', null, null),
(srvr_name, 'Disk Space Usage Monitor', 'disk-usage-all-log', '{disk-usage-warn-log}', '/home/disk-usage-monitor/disk-usage-monitor.log', NULL, '{WARNING}'),
;
end $$;

INSERT INTO logs_manager.alert_email_control VALUES (false, false);
INSERT INTO logs_manager.alert_settings (id_served_app, urgent_if_message_contains, email_to, bitrix_to) 
VALUES 
(1,'{WAR, ERR}','{giii.iggg2@gmail.com,sorokin@oculeus.com}','{129,121}'), 
(2,null,'{alert@oculeus.zohodesk.eu}','{chat2165}'),
(3,'{threshold exceeded}','{alert@oculeus.zohodesk.eu}','{chat2165}');

-----------------------------------------------------------------------------------------------------------------

--Для Windows:
INSERT INTO logs_manager.agent_bit_control (server_name) VALUES ('%s');
INSERT INTO logs_manager.served_application (server_name,service_name,service_tag_general,service_tags_alert,path_to_logfile,path_to_errlogfile,keywords_alert) 
VALUES ('%[1]s', 'CaptSyncService', 'CaptSyncService-all', '{CaptSyncService-fail,CaptSyncService-err}', 'C:\capturasystem\CaptSyncService_Log\*.log', 'C:\capturasystem\CaptSyncService_Log\*.log_error', '{FAIL,ERROR}');
INSERT INTO logs_manager.alert_email_control VALUES (false, false);
INSERT INTO logs_manager.alert_settings (id_served_app, urgent_if_message_contains, email_to) VALUES (1,'{FAIL, ERROR}','{alert@oculeus.zohodesk.eu}');

UPDATE logs_manager.alert_settings SET bitrix_to='{123}' WHERE id_served_app=2;

INSERT INTO logs_manager.served_application (server_name,service_name,service_tag_general,service_tags_alert,path_to_logfile,path_to_errlogfile,keywords_alert) 
VALUES
('Kaztranscom-Rater', 'Postgres-Agent', 'pgagent_all_log', '{pgagent_err_log}', '/home', null, null),
('Kaztranscom-Rater', 'Disk Space Usage Monitor', 'disk-usage-all-log', '{disk-usage-warn-log}', '/home/disk-usage-monitor/disk-usage-monitor.log', NULL, '{WARNING}'),
('Kaztranscom-Rater', 'Rater Backup', 'rater_backup_all_log', '{rater_backup_err_log}', '/home/rater-backup/logs/rater-backup.log', null, '{ERR}'),
('Kaztranscom-Rater', 'Agent-Bit Control', 'agent-bit-control_all_log', '{agent-bit-control_err_log}', '/home/logs-manager/logs/agent-bit-control.log', null, '{ERR}')
;


INSERT INTO logs_manager.alert_settings (id_served_app, urgent_if_message_contains, email_to, bitrix_to) 
VALUES  
(3,null,'{alert@oculeus.zohodesk.eu}','{chat2165}'), 
(4,'{threshold exceeded}','{alert@oculeus.zohodesk.eu}','{chat2165}'),
(5,'{ERR}','{sorokin@oculeus.com}','{121}'),
(6,'{ERR}','{sorokin@oculeus.com}','{121}')
; 

INSERT INTO logs_manager.agent_bit_control (server_name) VALUES ('Kaztranscom-Main');
INSERT INTO logs_manager.served_application (server_name,service_name,service_tag_general,service_tags_alert,path_to_logfile,path_to_errlogfile,keywords_alert) 
VALUES
('Kaztranscom-Main', 'Disk Space Monitor', 'disk-space-all-log', '{disk-space-warn-log}', '/home/disk-usage-monitor/disk-usage-monitor.log', NULL, '{WARNING}')
;

INSERT INTO logs_manager.alert_settings (id_served_app, urgent_if_message_contains, email_to, bitrix_to) 
VALUES (8,'{threshold exceeded}','{alert@oculeus.zohodesk.eu}','{chat2165}');

1 | Kaztranscom-Rater | Captura-Rating
2 | Kaztranscom-Rater | Prerate 
3 | Kaztranscom-Rater | Postgres-Agent
4 | Kaztranscom-Rater | Disk Space Usage 
5 | Kaztranscom-Rater | Rater Backup
6 | Kaztranscom-Rater | Agent-Bit Control