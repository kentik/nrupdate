package nrupdate

import (
	"context"
	"database/sql"
	"net/http"
	"os"

	"github.com/kentik/ktranslate"
	"github.com/kentik/ktranslate/pkg/eggs/baseserver"
	"github.com/kentik/ktranslate/pkg/eggs/logger"
	"github.com/kentik/ktranslate/pkg/kt"

	"github.com/judwhite/go-svc"

	_ "github.com/lib/pq" // Load postgres driver
)

const (
	PG_RW_CON = "PG_CONNECTION_RW"
	PG_RO_CON = "PG_CONNECTION"
)

type NRUpdate struct {
	logger.ContextL

	pgdb       *sql.DB
	pgdbRW     *sql.DB
	targetHost string
	cfg        *ktranslate.Config
}

func NewNRUpdate(targetHost string, cfg *ktranslate.Config, log logger.ContextL) (*NRUpdate, error) {
	return &NRUpdate{
		ContextL:   log,
		targetHost: targetHost,
		cfg:        cfg,
	}, nil
}

// GetStatus implements the baseserver.Service interface.
func (kc *NRUpdate) GetStatus() []byte {
	return []byte("OK")
}

// RunHealthCheck implements the baseserver.Service interface.
func (kc *NRUpdate) RunHealthCheck(ctx context.Context, result *baseserver.HealthCheckResult) {
}

// HttpInfo implements the baseserver.Service interface.
func (kc *NRUpdate) HttpInfo(w http.ResponseWriter, r *http.Request) {}

func (kc *NRUpdate) Run(ctx context.Context) error {
	defer kc.cleanup()

	// Connect PG
	if db, err := sql.Open("postgres", os.Getenv(PG_RO_CON)); err == nil {
		kc.pgdb = db
		kc.Infof("Connected to PG")
	} else {
		return err
	}

	if db, err := sql.Open("postgres", os.Getenv(PG_RW_CON)); err == nil {
		kc.pgdbRW = db
		kc.Infof("Connected to PG RW")
	} else {
		return err
	}

	// First update the device_alert field to send flow to the target host for any missing devices.
	if err := kc.updateNRAlerts(); err != nil {
		return err
	}

	// Then update our config file with the lastest info.
	if err := kc.updateConfigFile(); err != nil {
		return err
	}

	// Finally, write out the new config.
	return kc.cfg.SaveConfig()
}

// These are needed in case we are running under windows.
func (kc *NRUpdate) Init(env svc.Environment) error {
	return nil
}

func (kc *NRUpdate) Start() error {
	go kc.Run(context.Background())
	return nil
}

func (kc *NRUpdate) Stop() error {
	return kc.cleanup()
}

func (kc *NRUpdate) cleanup() error {
	if kc.pgdb != nil {
		kc.pgdb.Close()
	}
	if kc.pgdbRW != nil {
		kc.pgdbRW.Close()
	}

	return nil
}

func (n *NRUpdate) updateNRAlerts() error {
	res, err := n.pgdbRW.Exec(`
update
  mn_device set
    edate=now(),
    device_alert = '127.0.0.1:9456,$1'
where
  device_name = 'ksynth'
  and device_alert not like '%$1%'
  and company_id in (
    select id from mn_company
      where exist(company_kvs, 'nr_api_key') and company_status = 'V'
  )
`, n.targetHost)

	if err != nil {
		return err
	}

	count, err := res.RowsAffected()
	n.Infof("Updated %d alert devices", count)

	return nil
}

func (n *NRUpdate) updateConfigFile() error {
	rows, err := n.pgdb.Query(`
select
  a.id,
  company_kvs->'nr_api_key' as api_key,
  company_kvs->'nr_account_id' as account_id,
  user_email,
  user_kvs->'api_token' as kentik_api
from
    mn_company as a
  join
    mn_user as b
  on (a.id = b.company_id)
where
  exist(company_kvs, 'nr_api_key')
  and company_status = 'V'
  and user_email like 'ksynth-owners+%@kentik.com'
order by a.id
`)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Loop through rows, using Scan to assign column data to struct fields.
	newCreds := []ktranslate.KentikCred{}
	newNR := map[int]ktranslate.NRCred{}
	for rows.Next() {
		var companyID kt.Cid
		var kc ktranslate.KentikCred
		var nr ktranslate.NRCred

		if err := rows.Scan(&companyID, &nr.NRApiToken, &nr.NRAccount, &kc.APIEmail, &kc.APIToken); err != nil {
			return err
		}

		newCreds = append(newCreds, kc)
		newNR[int(companyID)] = nr
	}
	if err = rows.Err(); err != nil {
		return err
	}

	n.Infof("Found %d creds to map", len(newCreds))
	n.cfg.KentikCreds = newCreds
	n.cfg.NewRelicMultiSink.CredMap = newNR

	return nil
}
