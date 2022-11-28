package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kentik/nrupdate"

	"github.com/imdario/mergo"
	"github.com/kentik/ktranslate"
	"github.com/kentik/ktranslate/pkg/eggs/baseserver"
	"github.com/kentik/ktranslate/pkg/eggs/logger"
	"github.com/kentik/ktranslate/pkg/eggs/properties"
	"github.com/kentik/ktranslate/pkg/eggs/version"
)

func init() {

}

var (
	Version = version.VersionInfo{
		Version: "beta",
	}
)

func main() {
	var (
		targetHost     = flag.String("target-host", "", "Host and port to send NR alerts to.")
		configFilePath = flag.String("config", "", "Path to ktranslate config.")
	)

	// this is needed in order to catch the config options
	flag.Parse()

	// Grab a config default.
	cfg := ktranslate.DefaultConfig()

	// if config specified, merge config
	if v := *configFilePath; v != "" {
		ktCfg, err := ktranslate.LoadConfig(v)
		if err != nil {
			panic(err)
		}

		// merge passed with default
		if err := mergo.Merge(ktCfg, cfg); err != nil {
			panic(err)
		}

		cfg.Server.CfgPath = v
		cfg = ktCfg
	}

	bs := baseserver.BoilerplateWithPrefix("nrupdate", Version, "chf.nrupdate", properties.NewEnvPropertyBacking(), nil, cfg.Server)
	bs.BaseServerConfiguration.SkipEnvDump = true // Turn off dumping the envs on panic

	// Check this seperately down here because we need baseserver.
	if v := *configFilePath; v == "" {
		bs.Fail("Flag --config is required.")
	}
	if v := *targetHost; v == "" {
		bs.Fail("Flag --target-host is required.")
	}

	prefix := fmt.Sprintf("NRUpdate")
	lc := logger.NewContextLFromUnderlying(logger.SContext{S: prefix}, bs.Logger)

	nu, err := nrupdate.NewNRUpdate(*targetHost, cfg, lc)
	if err != nil {
		bs.Fail(fmt.Sprintf("Cannot start nrupdate: %v", err))
	}

	lc.Infof("CLI: %v", os.Args)
	bs.Run(nu)
}
