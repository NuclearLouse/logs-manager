ALTER TABLE logs_manager.alert_settings ADD bitrix_to _text NULL;
DROP FUNCTION logs_manager.served_app_alert_settings(in_served_app_id integer);