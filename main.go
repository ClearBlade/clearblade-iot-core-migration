package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	cbiotcore "github.com/clearblade/go-iot"
)

const TotalWorkers = 25

var (
	Args DeviceMigratorArgs
)

var (
	colorCyan   = "\033[36m"
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
)

type DeviceMigratorArgs struct {
	// ClearBlade destination specific flags
	cbServiceAccount string
	cbRegistryName   string
	cbRegistryRegion string

	// ClearBlade source specific flags
	cbSourceServiceAccount string
	cbSourceRegistryName   string
	cbSourceRegion         string

	// Optional flags
	devicesCsvFile    string
	configHistory     bool
	updatePublicKeys  bool
	skipConfig        bool
	silentMode        bool
	cleanupCbRegistry bool
	exportBatchSize   int64
}

func initMigrationFlags() {
	flag.StringVar(&Args.cbServiceAccount, "cbServiceAccount", "", "Path to a ClearBlade service account file for the destination registry. See https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project (Required)")
	flag.StringVar(&Args.cbRegistryName, "cbRegistryName", "", "ClearBlade Destination Registry Name (Required)")
	flag.StringVar(&Args.cbRegistryRegion, "cbRegistryRegion", "", "ClearBlade Destination Registry Region (Required)")

	flag.StringVar(&Args.cbSourceServiceAccount, "cbSourceServiceAccount", "", "Path to a ClearBlade service account file for the source registry. See https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project (Required)")
	flag.StringVar(&Args.cbSourceRegistryName, "cbSourceRegistryName", "", "ClearBlade Source Registry Name (Required)")
	flag.StringVar(&Args.cbSourceRegion, "cbSourceRegion", "", "ClearBlade Source Registry Region (Required)")

	// Optional
	flag.StringVar(&Args.devicesCsvFile, "devicesCsv", "", "Devices CSV file path. Device ids in column: deviceId")
	flag.BoolVar(&Args.configHistory, "configHistory", true, "Store Config History. Default is true")
	flag.BoolVar(&Args.updatePublicKeys, "updatePublicKeys", true, "Replace existing keys of migrated devices. Default is true")
	flag.BoolVar(&Args.skipConfig, "skipConfig", false, "Skips migrating latest config. Default is false")
	flag.BoolVar(&Args.silentMode, "silentMode", false, "Run this tool in silent (non-interactive) mode. Default is false")
	flag.BoolVar(&Args.cleanupCbRegistry, "cleanupCbRegistry", false, "Cleans up all contents from the existing CB registry prior to migration")
	flag.Int64Var(&Args.exportBatchSize, "exportBatchSize", 0, "Exports devices to the supplied number of csvs")

}

func main() {

	// Init & Parse migration Flags
	initMigrationFlags()
	flag.Parse()

	if len(os.Args) == 1 {
		log.Fatalln("No flags supplied. Use clearblade-iot-core-migration --help to view details.")
	}

	if os.Args[1] == "version" {
		fmt.Printf("%s\n", cbIotCoreMigrationVersion)
		os.Exit(0)
	}

	if runtime.GOOS == "windows" {
		colorCyan = ""
		colorReset = ""
		colorGreen = ""
		colorYellow = ""
		colorRed = ""
	}

	// Validate if all required CB source flags are provided
	fmt.Println(string(colorGreen), "\n\u2713 Validating source flags", string(colorReset))
	validateSourceCBFlags()

	fmt.Println(string(colorCyan), "\n\n================= Starting Device Migration =================\n\nRunning Version: ", cbIotCoreMigrationVersion, "\n\n", string(colorReset))

	// Authenticate CB source and destination accounts
	sourceCtx := context.Background()
	sourceService, err := cbiotcore.NewService(sourceCtx)
	if err != nil {
		log.Fatalln(err)
	}

	sourceRegDetails, err := cbiotcore.GetRegistryCredentials(Args.cbSourceRegistryName, Args.cbSourceRegion, sourceService)

	if sourceRegDetails.SystemKey == "" {
		fmt.Println(string(colorRed), "\n\u2715 Unable to fetch ClearBlade source registry Details! Please check if -cbSourceRegistryName and/or -cbSourceRegion flags are set correctly.")
		os.Exit(0)
	}
	devices, deviceConfigs := fetchDevicesFromClearBladeIotCore(sourceCtx, sourceService)

	if Args.exportBatchSize != 0 {
		ExportDeviceBatches(devices, Args.exportBatchSize)
		fmt.Println(string(colorGreen), "\n\n\u2713 Device batches exported to csv!", string(colorReset))
		return
	}

	// Validate if all required CB destination flags are provided
	fmt.Println(string(colorGreen), "\n\u2713 Validating destination flags", string(colorReset))
	validateCBFlags(Args.cbSourceRegion)

	fmt.Println(string(colorGreen), "\n\u2713 All Flags validated!", string(colorReset))

	destinationCtx := context.Background()
	destinationService, err := cbiotcore.NewService(destinationCtx)
	if err != nil {
		log.Fatal(err)
	}

	regDetails, _ := cbiotcore.GetRegistryCredentials(Args.cbRegistryName, Args.cbRegistryRegion, destinationService)
	if regDetails.SystemKey == "" {
		fmt.Println(string(colorRed), "\n\u2715 Unable to fetch ClearBlade destination registry Details! Please check if -cbRegistryName and/or -cbRegistryRegion flags are set correctly.")
		os.Exit(0)
	}

	// Fetch devices from the given registry
	errorLogs := make([]ErrorLog, 0)

	if Args.cleanupCbRegistry {
		deleteAllFromCbRegistry(destinationService)
		fmt.Println(string(colorGreen), "\n\n\u2713 Successfully Cleaned up destination ClearBlade registry!\n", string(colorReset))
	}

	// Add fetched devices to ClearBlade Device table
	errorLogs = addDevicesToClearBlade(destinationService, devices, deviceConfigs, errorLogs)

	migrateBoundDevicesToClearBlade(destinationService, sourceService, devices, errorLogs)

	if len(errorLogs) > 0 {
		if err := generateFailedDevicesCSV(errorLogs); err != nil {
			log.Fatalln(err)
		}
	}

	fmt.Println(string(colorGreen), "\n\n\u2713 Migration complete!", string(colorReset))

}

func validateSourceCBFlags() {
	if Args.cbSourceServiceAccount == "" {
		if Args.silentMode {
			log.Fatalln("-cbSourceServiceAccount is required parameter")
		}
		value, err := readInput("Enter ClearBlade Source Service Account File path (.json): ")
		if err != nil {
			log.Fatalln("Error reading service account file path: ", err)
		}
		Args.cbSourceServiceAccount = value
	}

	// validate that path to service account file exists
	if _, err := os.Stat(Args.cbSourceServiceAccount); errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Could not location service account file %s. Please make sure the path is correct\n", Args.cbSourceServiceAccount)
	}

	err := os.Setenv("CLEARBLADE_CONFIGURATION", Args.cbSourceServiceAccount)
	if err != nil {
		log.Fatalln("Failed to set CLEARBLADE_CONFIGURATION env variable", err.Error())
	}

	if Args.cbSourceRegistryName == "" {
		if Args.silentMode {
			log.Fatalln("-cbSourceRegistryName is required parameter")
		}
		value, err := readInput("Enter ClearBlade Source Registry Name: ")
		if err != nil {
			log.Fatalln("Error reading source registry name: ", err)
		}
		Args.cbSourceRegistryName = value
	}

	if Args.cbSourceRegion == "" {
		if Args.silentMode {
			log.Fatalln("-cbSourceRegion is required parameter")
		}
		value, err := readInput("Enter ClearBlade Source Registry Region: ")
		if err != nil {
			log.Fatalln("Error reading source registry region: ", err)
		}
		Args.cbSourceRegion = value
	}

	if Args.devicesCsvFile == "" {
		if Args.silentMode {
			return
		}
		value, err := readInput("Enter Devices CSV file path (By default all devices from the registry will be migrated. Press enter to skip!): ")
		if err != nil {
			log.Fatalln("Error reading service account file path: ", err)
		}
		Args.devicesCsvFile = value
	}
}

func validateCBFlags(registryRegion string) {

	fmt.Println(string(colorGreen), "\n\u2713 Validating service account flag", string(colorReset))
	if Args.cbServiceAccount == "" {
		if Args.silentMode {
			log.Fatalln("-cbServiceAccount is a required paramter")
		}

		value, err := readInput("Enter path to ClearBlade service account file. See https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project for more info: ")
		if err != nil {
			log.Fatalln("Error reading service account: ", err)
		}
		Args.cbServiceAccount = value
	}

	// validate that path to service account file exists
	fmt.Println(string(colorGreen), "\n\u2713 Validating service account location", string(colorReset))
	if _, err := os.Stat(Args.cbServiceAccount); errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Could not location service account file %s. Please make sure the path is correct\n", Args.cbServiceAccount)
	}

	fmt.Println(string(colorGreen), "\n\u2713 Setting environment variable CLEARBLADE_CONFIGURATION", string(colorReset))
	err := os.Setenv("CLEARBLADE_CONFIGURATION", Args.cbServiceAccount)
	if err != nil {
		log.Fatalln("Failed to set CLEARBLADE_CONFIGURATION env variable", err.Error())
	}

	fmt.Println(string(colorGreen), "\n\u2713 Validating registry name", string(colorReset))
	if Args.cbRegistryName == "" {
		if Args.silentMode {
			log.Fatalln("-cbRegistryName is required parameter")
		}
		value, err := readInput("Enter ClearBlade Registry Name: ")
		if err != nil {
			log.Fatalln("Error reading registry name: ", err)
		}
		Args.cbRegistryName = value
	}

	fmt.Println(string(colorGreen), "\n\u2713 Validating registry region", string(colorReset))
	if Args.cbRegistryRegion == "" {
		if Args.silentMode {
			Args.cbRegistryRegion = Args.cbSourceRegion
			// log.Fatalln("-cbRegistryRegion is required parameter")
		}
		value, err := readInput("Enter ClearBlade Registry Region (Press enter to skip if you are migrating to the same region): ")
		if err != nil {
			log.Fatalln("Error reading registry region: ", err)
		}

		if value == "" {
			Args.cbRegistryRegion = registryRegion
		} else {
			Args.cbRegistryRegion = value
		}

	}

}
