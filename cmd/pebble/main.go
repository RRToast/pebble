package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/RRToast/pebble/ca"
	"github.com/RRToast/pebble/cmd"
	"github.com/RRToast/pebble/db"
	"github.com/RRToast/pebble/va"
	"github.com/RRToast/pebble/wfe"
)

type config struct {
	Pebble struct {
		ListenAddress           string
		ManagementListenAddress string
		HTTPPort                int
		TLSPort                 int
		Certificate             string
		PrivateKey              string
		OCSPResponderURL        string
		// Require External Account Binding for "newAccount" requests
		ExternalAccountBindingRequired bool
		ExternalAccountMACKeys         map[string]string
		// Configure policies to deny certain domains
		DomainBlocklist []string
	}
}

func main() {
	println("Es geht los")
	configFile := flag.String(
		"config",
		"test/config/pebble-config.json",
		"File path to the Pebble configuration file")
	strictMode := flag.Bool(
		"strict",
		false,
		"Enable strict mode to test upcoming API breaking changes")
	resolverAddress := flag.String(
		"dnsserver",
		"",
		"Define a custom DNS server address (ex: 192.168.0.56:5053 or 8.8.8.8:53).")
	flag.Parse()
	if *configFile == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Log to stdout
	logger := log.New(os.Stdout, "Pebble ", log.LstdFlags)
	logger.Printf("Starting Pebble ACME server")

	var c config
	err := cmd.ReadConfigFile(*configFile, &c)
	cmd.FailOnError(err, "Reading JSON config file into config structure")

	// solange nicht positiver Wert uninteressant.
	alternateRoots := 0
	alternateRootsVal := os.Getenv("PEBBLE_ALTERNATE_ROOTS")
	if val, err := strconv.ParseInt(alternateRootsVal, 10, 0); err == nil && val >= 0 {
		alternateRoots = int(val)
	}
	println(alternateRootsVal)

	chainLength := 1
	if val, err := strconv.ParseInt(os.Getenv("PEBBLE_CHAIN_LENGTH"), 10, 0); err == nil && val >= 0 {
		chainLength = int(val)
	}

	db := db.NewMemoryStore()
	ca := ca.New(logger, db, c.Pebble.OCSPResponderURL, alternateRoots, chainLength)
	va := va.New(logger, c.Pebble.HTTPPort, c.Pebble.TLSPort, *strictMode, *resolverAddress)

	for keyID, key := range c.Pebble.ExternalAccountMACKeys {
		err := db.AddExternalAccountKeyByID(keyID, key)
		cmd.FailOnError(err, "Failed to add key to external account bindings")
	}

	for _, domainName := range c.Pebble.DomainBlocklist {
		err := db.AddBlockedDomain(domainName)
		cmd.FailOnError(err, "Failed to add domain to block list")
	}

	wfeImpl := wfe.New(logger, db, va, ca, *strictMode, c.Pebble.ExternalAccountBindingRequired)
	muxHandler := wfeImpl.Handler()

	if c.Pebble.ManagementListenAddress != "" {
		go func() {
			adminHandler := wfeImpl.ManagementHandler()
			err = http.ListenAndServeTLS(
				c.Pebble.ManagementListenAddress,
				c.Pebble.Certificate,
				c.Pebble.PrivateKey,
				adminHandler)
			cmd.FailOnError(err, "Calling ListenAndServeTLS() for admin interface")
		}()
		logger.Printf("Management interface listening on: %s\n", c.Pebble.ManagementListenAddress)
		logger.Printf("Root CA certificate available at: https://%s%s0",
			c.Pebble.ManagementListenAddress, wfe.RootCertPath)
		for i := 0; i < alternateRoots; i++ {
			logger.Printf("Alternate (%d) root CA certificate available at: https://%s%s%d",
				i+1, c.Pebble.ManagementListenAddress, wfe.RootCertPath, i+1)
		}
	} else {
		logger.Print("Management interface is disabled")
	}

	logger.Printf("Listening on: %s\n", c.Pebble.ListenAddress)
	logger.Printf("ACME directory available at: https://%s%s",
		c.Pebble.ListenAddress, wfe.DirectoryPath)
	err = http.ListenAndServeTLS(
		c.Pebble.ListenAddress,
		c.Pebble.Certificate,
		c.Pebble.PrivateKey,
		muxHandler)
	cmd.FailOnError(err, "Calling ListenAndServeTLS()")
}
