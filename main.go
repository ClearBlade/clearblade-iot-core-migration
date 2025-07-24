package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	cbiotcore "github.com/clearblade/go-iot"
)

const (
	cbIotCoreMigrationVersion = "v1.6.0"
	TotalWorkers              = 25
)

var (
	Args        DeviceMigratorArgs
	errorLogger = NewErrorLogger()
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
	workDir           string
}

func initMigrationFlags() {
	// Destination
	flag.StringVar(&Args.cbServiceAccount, "cbServiceAccount", "", "Path to a ClearBlade service account file for the destination registry. See https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project (Required)")
	flag.StringVar(&Args.cbRegistryName, "cbRegistryName", "", "ClearBlade Destination Registry Name (Required)")
	flag.StringVar(&Args.cbRegistryRegion, "cbRegistryRegion", "", "ClearBlade Destination Registry Region (Required)")

	// Source
	flag.StringVar(&Args.cbSourceServiceAccount, "cbSourceServiceAccount", "", "Path to a ClearBlade service account file for the source registry. See https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project (Required)")
	flag.StringVar(&Args.cbSourceRegistryName, "cbSourceRegistryName", "", "ClearBlade Source Registry Name (Required)")
	flag.StringVar(&Args.cbSourceRegion, "cbSourceRegion", "", "ClearBlade Source Registry Region (Required)")

	// Optional
	flag.StringVar(&Args.devicesCsvFile, "devicesCsv", "", "Devices CSV file path. Device ids in column: deviceId")
	flag.BoolVar(&Args.configHistory, "configHistory", true, "Store Config History. Default is true")
	flag.BoolVar(&Args.updatePublicKeys, "updatePublicKeys", true, "Replace existing keys of migrated devices. Default is true")
	flag.BoolVar(&Args.skipConfig, "skipConfig", false, "Skips migrating latest config. Default is false")
	flag.BoolVar(&Args.silentMode, "silentMode", false, "Run this tool in silent (non-interactive) mode. Default is false")
	flag.BoolVar(&Args.cleanupCbRegistry, "cleanupCbRegistry", false, "Deletes all contents from the destination CB registry prior to migration")
	flag.Int64Var(&Args.exportBatchSize, "exportBatchSize", 0, "Exports devices to the supplied number of CSVs")
	flag.StringVar(&Args.workDir, "workDir", "./migration_data", "Directory to store migration data")

	flag.Parse()
}

func validateSourceCBFlags() {
	if Args.cbSourceServiceAccount == "" {
		if Args.silentMode {
			log.Fatalln("-cbSourceServiceAccount is a required parameter")
		}
		value, err := readInput("Enter ClearBlade Source Service Account File path (.json): ")
		if err != nil {
			log.Fatalln("Error reading service account file path: ", err)
		}
		Args.cbSourceServiceAccount = value
	}

	// validate that path to service account file exists
	if _, err := os.Stat(Args.cbSourceServiceAccount); errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Could not locate service account file %s. Please make sure the path is correct", Args.cbSourceServiceAccount)
	}

	if Args.cbSourceRegistryName == "" {
		if Args.silentMode {
			log.Fatalln("-cbSourceRegistryName is a required parameter")
		}
		value, err := readInput("Enter ClearBlade Source Registry Name: ")
		if err != nil {
			log.Fatalln("Error reading source registry name: ", err)
		}
		Args.cbSourceRegistryName = value
	}

	if Args.cbSourceRegion == "" {
		if Args.silentMode {
			log.Fatalln("-cbSourceRegion is a required parameter")
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
	printfColored(colorGreen, "\u2713 Validating service account flag")
	if Args.cbServiceAccount == "" {
		if Args.silentMode {
			log.Fatalln("-cbServiceAccount is a required parameter")
		}

		value, err := readInput("Enter path to ClearBlade service account file. See https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project for more info: ")
		if err != nil {
			log.Fatalln("Error reading service account: ", err)
		}
		Args.cbServiceAccount = value
	}

	// validate that path to service account file exists
	printfColored(colorGreen, "\u2713 Validating service account location")
	if _, err := os.Stat(Args.cbServiceAccount); errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Could not locate service account file %s. Please make sure the path is correct", Args.cbServiceAccount)
	}

	printfColored(colorGreen, "\u2713 Validating registry name")
	if Args.cbRegistryName == "" {
		if Args.silentMode {
			log.Fatalln("-cbRegistryName is a required parameter")
		}
		value, err := readInput("Enter ClearBlade Registry Name: ")
		if err != nil {
			log.Fatalln("Error reading registry name: ", err)
		}
		Args.cbRegistryName = value
	}

	printfColored(colorGreen, "\u2713 Validating registry region")
	if Args.cbRegistryRegion == "" {
		if Args.silentMode {
			Args.cbRegistryRegion = Args.cbSourceRegion
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

func getIoTCoreService(serviceAccountFilePath string) (*cbiotcore.Service, error) {
	err := os.Setenv("CLEARBLADE_CONFIGURATION", serviceAccountFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to set CLEARBLADE_CONFIGURATION env variable: %w", err)
	}
	return cbiotcore.NewService(context.Background())
}

func verifyRegistryDetails(service *cbiotcore.Service, registryName, region string) error {
	regDetails, err := cbiotcore.GetRegistryCredentials(registryName, region, service)
	if err != nil {
		return err
	}
	if regDetails.SystemKey == "" {
		return fmt.Errorf("no system key found in registry %s (region: %s)", registryName, region)
	}
	return nil
}

func main() {
	if len(os.Args) == 1 {
		log.Fatalln("No flags supplied. Use clearblade-iot-core-migration --help to view details.")
	}

	if os.Args[1] == "version" {
		fmt.Println(cbIotCoreMigrationVersion)
		os.Exit(0)
	}

	initMigrationFlags()

	printfColored(colorGreen, "\u2713 Validating source flags")
	validateSourceCBFlags()
	printfColored(colorGreen, "\u2713 Validating destination flags")
	validateCBFlags(Args.cbSourceRegion)

	printfColored(colorGreen, "\u2713 All Flags validated!")
	printfColored(colorCyan, "================= Starting Device Migration =================\nRunning Version: %s\n", cbIotCoreMigrationVersion)

	// --------------------- Fetch data from source ---------------------

	sourceService, err := getIoTCoreService(Args.cbSourceServiceAccount)
	if err != nil {
		log.Fatalf("Unable to connect to source registry: %s\n", err)
	}
	err = verifyRegistryDetails(sourceService, Args.cbSourceRegistryName, Args.cbSourceRegion)
	if err != nil {
		log.Fatalf("Error verifying registry details: %s\n", err)
	}

	devices := fetchDevices(sourceService)
	deviceConfigs := fetchConfigHistory(sourceService, devices)
	gatewayBindings := fetchGatewayBindings(sourceService, devices)

	if Args.exportBatchSize != 0 { // TODO
		ExportDeviceBatches(devices, Args.exportBatchSize)
		printfColored(colorGreen, "\u2713 Device batches exported to csv!")
		return
	}

	// --------------------- Push data to destination ---------------------

	destinationService, err := getIoTCoreService(Args.cbServiceAccount)
	if err != nil {
		log.Fatalf("Unable to connect to destination registry: %s\n", err)
	}
	err = verifyRegistryDetails(destinationService, Args.cbRegistryName, Args.cbRegistryRegion)
	if err != nil {
		log.Fatalf("Error verifying destination registry details: %s\n", err)
	}

	defer errorLogger.WriteToFile()

	if Args.cleanupCbRegistry {
		deleteAllFromCbRegistry(destinationService)
		printfColored(colorGreen, "\u2713 Successfully Cleaned up destination ClearBlade registry!")
	}

	addDevicesToClearBlade(destinationService, devices, deviceConfigs)
	migrateBoundDevicesToClearBlade(destinationService, gatewayBindings)

	printfColored(colorGreen, "\u2713 Migration complete!")
}
