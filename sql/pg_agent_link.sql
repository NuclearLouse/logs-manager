CREATE SERVER IF NOT EXISTS job_agent
    FOREIGN DATA WRAPPER postgres_fdw
    OPTIONS (dbname 'postgres', host '127.0.0.1', port '5432', fetch_size '1000');
ALTER SERVER job_agent OWNER TO ocuconnection;
CREATE USER MAPPING IF NOT EXISTS FOR postgres SERVER job_agent OPTIONS (password '', "user" 'postgres');
CREATE USER MAPPING IF NOT EXISTS FOR ocuconnection SERVER job_agent OPTIONS (password '', "user" 'postgres');		
	
CREATE FOREIGN TABLE IF NOT EXISTS public.pga_job(
    jobid integer NULL,
    jobname text NULL COLLATE pg_catalog."default",
    jobenabled boolean NULL,
    jobnextrun timestamp with time zone NULL,
    joblastrun timestamp with time zone NULL
)
    SERVER job_agent
    OPTIONS (schema_name 'pgagent', table_name 'pga_job');
ALTER FOREIGN TABLE public.pga_job OWNER TO ocuconnection;
		
CREATE FOREIGN TABLE IF NOT EXISTS public.pga_jobsteplog(
    jslid integer NOT NULL,
    jsljlgid integer NOT NULL,
    jsljstid integer NOT NULL,
    jslstatus character(1) NOT NULL COLLATE pg_catalog."default",
    jslresult integer NULL,
    jslstart timestamp with time zone NOT NULL,
    jslduration interval NULL,
    jsloutput text NULL COLLATE pg_catalog."default"
)
    SERVER job_agent
    OPTIONS (schema_name 'pgagent', table_name 'pga_jobsteplog');
ALTER FOREIGN TABLE public.pga_jobsteplog OWNER TO ocuconnection;

CREATE FOREIGN TABLE IF NOT EXISTS public.pga_schedule(
    jscid integer NOT NULL,
    jscjobid integer NOT NULL,
    jscname text NOT NULL COLLATE pg_catalog."default",
    jscdesc text NOT NULL COLLATE pg_catalog."default",
    jscenabled boolean NOT NULL,
    jscstart timestamp with time zone NOT NULL,
    jscend timestamp with time zone NULL,
    jscminutes boolean[] NOT NULL,
    jschours boolean[] NOT NULL,
    jscweekdays boolean[] NOT NULL,
    jscmonthdays boolean[] NOT NULL,
    jscmonths boolean[] NOT NULL
)
    SERVER job_agent
    OPTIONS (schema_name 'pgagent', table_name 'pga_schedule');
ALTER FOREIGN TABLE public.pga_schedule OWNER TO ocuconnection;

CREATE FOREIGN TABLE IF NOT EXISTS public.pga_jobstep (
    "jstid" INTEGER NOT NULL,
    "jstjobid" INTEGER NOT NULL,
    "jstname" TEXT NOT NULL,
    "jstdesc" TEXT NOT NULL,
    "jstenabled" BOOLEAN NOT NULL,
    "jstkind" CHARACTER(1) NOT NULL,
    "jstcode" TEXT NOT NULL,
    "jstconnstr" TEXT NOT NULL,
    "jstdbname" NAME NOT NULL,
    "jstonerror" CHARACTER(1)NOT NULL,
    "jscnextrun" TIMESTAMP WITH TIME ZONE
)
    SERVER job_agent
    OPTIONS (schema_name 'pgagent', table_name 'pga_jobstep');
ALTER FOREIGN TABLE public.pga_jobstep OWNER TO ocuconnection;


CREATE OR REPLACE FUNCTION public.pgagent_jobs_check()
 RETURNS VOID
 LANGUAGE plpgsql
AS $function$
/*alex*/
DECLARE
	pgagent_tag varchar = 'pgagent_err_log';
BEGIN	
	SET ROLE postgres;
	WITH err AS (SELECT  
			CASE 
				WHEN NOT bl_job_ok THEN format('[ERROR] %s pgAgent job "%s"[%s] status: "%s" last run: %s msg: %s', now(), jobname, jobid, l.status, l.last_run, l.job_error)
				WHEN NOT bl_job_running THEN format('[FAIL] %s pgAgent job "%s"[%s] status: "%s" idle for: %s last run: %s msg: %s', now(), jobname, jobid, l.status, now() - last_run, l.last_run, l.job_error)
				ELSE '' 
				END AS log,
			extract(epoch from now()) AS date
		FROM pga_job j
		JOIN pga_jobstep s on j.jobid = s.jstjobid
		LEFT JOIN (SELECT jsljstid, jslstatus status, jslstart last_run, jsloutput job_error, jslstart > (current_date -'12 HOUR'::INTERVAL) job_running, jslstatus = 's' job_ok,
				row_number() over(partition by jsljstid order by jslid desc) job_no
			FROM pga_jobsteplog l) l ON s.jstid = l.jsljstid and l.job_no = 1,
		LATERAL coalesce(l.job_ok, false) bl_job_ok,
		LATERAL coalesce(l.job_running, false) bl_job_running
		WHERE jobenabled AND (NOT bl_job_ok OR NOT bl_job_running)
		)
	INSERT INTO logs_manager.alert_logs (tag, "time", "data")
		SELECT pgagent_tag AS tag, now() AS "time", row_to_json(err)::jsonb AS "data" 
		FROM err;
	RESET ROLE;
END
$function$;
