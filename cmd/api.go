package cmd

import (
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/flashbots/boost-relay/api"
	"github.com/flashbots/boost-relay/beaconclient"
	"github.com/flashbots/boost-relay/common"
	"github.com/flashbots/boost-relay/datastore"
	"github.com/flashbots/go-boost-utils/bls"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	// defaults
	defaultListenAddr         = "localhost:9062"
	defaultBeaconURI          = common.GetEnv("BEACON_URI", "")
	defaultredisURI           = common.GetEnv("REDIS_URI", "localhost:6379")
	defaultLogJSON            = os.Getenv("LOG_JSON") != ""
	defaultLogLevel           = common.GetEnv("LOG_LEVEL", "info")
	defaultGenesisForkVersion = os.Getenv("GENESIS_FORK_VERSION")

	listenAddr    string
	beaconNodeURI string
	redisURI      string
	logJSON       bool
	logLevel      string

	useGenesisForkVersionMainnet bool
	useGenesisForkVersionKiln    bool
	useGenesisForkVersionRopsten bool
	useGenesisForkVersionSepolia bool
	useCustomGenesisForkVersion  string

	apiPprof bool

	secretKey           string
	getHeaderWaitTimeMs int64
)

func init() {
	rootCmd.AddCommand(apiCmd)
	apiCmd.Flags().StringVar(&listenAddr, "listen-addr", defaultListenAddr, "listen address for webserver")
	apiCmd.Flags().StringVar(&beaconNodeURI, "beacon-uri", defaultBeaconURI, "beacon endpoint")
	apiCmd.Flags().StringVar(&redisURI, "redis-uri", defaultredisURI, "redis uri")
	apiCmd.Flags().BoolVar(&apiPprof, "pprof", false, "enable pprof API")
	apiCmd.Flags().Int64Var(&getHeaderWaitTimeMs, "getheader-wait-ms", 500, "ms to wait on getHeader requests")
	apiCmd.Flags().StringVar(&secretKey, "secret-key", "", "secret key for signing bids")

	apiCmd.Flags().BoolVar(&logJSON, "json", defaultLogJSON, "log in JSON format instead of text")
	apiCmd.Flags().StringVar(&logLevel, "loglevel", defaultLogLevel, "log-level: trace, debug, info, warn/warning, error, fatal, panic")

	apiCmd.Flags().BoolVar(&useGenesisForkVersionMainnet, "mainnet", false, "use Mainnet genesis fork version 0x00000000 (for signature validation)")
	apiCmd.Flags().BoolVar(&useGenesisForkVersionKiln, "kiln", false, "use Kiln genesis fork version 0x70000069 (for signature validation)")
	apiCmd.Flags().BoolVar(&useGenesisForkVersionRopsten, "ropsten", false, "use Ropsten genesis fork version 0x80000069 (for signature validation)")
	apiCmd.Flags().BoolVar(&useGenesisForkVersionSepolia, "sepolia", false, "use Sepolia genesis fork version 0x90000069 (for signature validation)")
	apiCmd.Flags().StringVar(&useCustomGenesisForkVersion, "genesis-fork-version", defaultGenesisForkVersion, "use a custom genesis fork version (for signature validation)")
	apiCmd.MarkFlagsMutuallyExclusive("mainnet", "kiln", "ropsten", "sepolia", "genesis-fork-version")

	apiCmd.Flags().SortFlags = false
}

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Start the API server",
	Run: func(cmd *cobra.Command, args []string) {
		var err error

		common.LogSetup(logJSON, logLevel)
		log := logrus.WithField("module", "cmd/api")
		log.Infof("boost-relay %s", Version)

		// Set genesis fork version
		genesisForkVersionHex := ""
		if useCustomGenesisForkVersion != "" {
			genesisForkVersionHex = useCustomGenesisForkVersion
		} else if useGenesisForkVersionMainnet {
			genesisForkVersionHex = common.GenesisForkVersionMainnet
		} else if useGenesisForkVersionKiln {
			genesisForkVersionHex = common.GenesisForkVersionKiln
		} else if useGenesisForkVersionRopsten {
			genesisForkVersionHex = common.GenesisForkVersionRopsten
		} else if useGenesisForkVersionSepolia {
			genesisForkVersionHex = common.GenesisForkVersionSepolia
		} else {
			log.Fatal("Please specify a genesis fork version (eg. -mainnet or -kiln or -ropsten or -genesis-fork-version flags)")
		}
		log.Infof("Using genesis fork version: %s", genesisForkVersionHex)

		// Connect to beacon client and ensure it's synced
		log.Infof("Using beacon endpoint: %s", beaconNodeURI)
		beaconClient := beaconclient.NewProdBeaconClient(log, beaconNodeURI)
		_, err = beaconClient.SyncStatus()
		if err != nil {
			log.WithError(err).Fatal("Beacon node is syncing")
		}

		// Connect to Redis and setup the datastore
		redis, err := datastore.NewRedisCache(redisURI)
		if err != nil {
			log.WithError(err).Fatalf("Failed to connect to Redis at %s", redisURI)
		}
		log.Infof("Connected to Redis at %s", redisURI)
		ds := datastore.NewProdProposerDatastore(redis)

		// Decode the private key
		envSkBytes, err := hexutil.Decode(secretKey)
		if err != nil {
			log.WithError(err).Fatal("incorrect secret key provided")
		}
		sk, err := bls.SecretKeyFromBytes(envSkBytes[:])
		if err != nil {
			log.WithError(err).Fatal("incorrect builder API secret key provided")
		}

		opts := api.RelayAPIOpts{
			Log:                   log,
			ListenAddr:            listenAddr,
			BeaconClient:          beaconClient,
			Datastore:             ds,
			GenesisForkVersionHex: genesisForkVersionHex,
			PprofAPI:              apiPprof,
			GetHeaderWaitTime:     time.Duration(getHeaderWaitTimeMs) * time.Millisecond,
			SecretKey:             sk,
		}

		// Create the relay service
		srv, err := api.NewRelayAPI(opts)
		if err != nil {
			log.WithError(err).Fatal("failed to create service")
		}

		// Start the server
		log.Infof("Webserver starting on %s ...", listenAddr)
		log.Fatal(srv.StartServer())
	},
}
