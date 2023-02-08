package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	gcpiotcore "cloud.google.com/go/iot/apiv1"
	cbiotcore "github.com/clearblade/go-iot"
)

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
	// ClearBlade specific flags
	cbServiceAccount string
	cbRegistryName   string
	cbRegistryRegion string

	// GCP IoT Core specific flags
	serviceAccountFile string
	registryName       string
	gcpRegistryRegion  string

	// Optional flags
	devicesCsvFile   string
	configHistory    bool
	updatePublicKeys bool
	skipConfig       bool
}

func initMigrationFlags() {
	flag.StringVar(&Args.cbServiceAccount, "cbServiceAccount", "", "Path to a ClearBlade service account file. See https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project (Required)")
	flag.StringVar(&Args.cbRegistryName, "cbRegistryName", "", "ClearBlade Registry Name (Required)")
	flag.StringVar(&Args.cbRegistryRegion, "cbRegistryRegion", "", "ClearBlade Registry Region (Required)")

	flag.StringVar(&Args.serviceAccountFile, "gcpServiceAccount", "", "Service account file path (Required)")
	flag.StringVar(&Args.registryName, "gcpRegistryName", "", "Google Registry Name (Required)")
	flag.StringVar(&Args.gcpRegistryRegion, "gcpRegistryRegion", "", "Google Registry Region (Required)")

	// Optional
	flag.StringVar(&Args.devicesCsvFile, "devicesCsv", "", "Devices CSV file path")
	flag.BoolVar(&Args.configHistory, "configHistory", false, "Store Config History. Default is false")
	flag.BoolVar(&Args.updatePublicKeys, "updatePublicKeys", true, "Replace existing keys of migrated devices. Default is true")
	flag.BoolVar(&Args.skipConfig, "skipConfig", false, "Skips migrating latest config. Default is false")
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

	fmt.Println(string(colorCyan), "\n\n================= Starting Device Migration =================\n\nRunning Version: ", cbIotCoreMigrationVersion, "\n\n", string(colorReset))

	// Validate if all required Google IOT Core flags are provided
	validateGCPIoTCoreFlags()

	// Validate if all required CB flags are provided
	validateCBFlags(Args.gcpRegistryRegion)

	fmt.Println(string(colorGreen), "\n\u2713 All Flags validated!", string(colorReset))

	// Authenticate GCP service user and Clearblade User account
	ctx, gcpClient, err := authenticate()

	defer gcpClient.Close()

	if err != nil {
		log.Fatalln(err)
	}

	// Fetch devices from the given registry
	devices, deviceConfigs := fetchDevicesFromGoogleIotCore(ctx, gcpClient)

	fmt.Println(string(colorCyan), "\nPreparing Device Migration\n", string(colorReset))

	cbCtx := context.Background()
	service, err := cbiotcore.NewService(cbCtx)
	if err != nil {
		log.Fatal(err)
	}

	errorLogs := make([]ErrorLog, 0)

	// Add fetched devices to ClearBlade Device table
	errorLogs = addDevicesToClearBlade(service, devices, deviceConfigs, errorLogs)

	migrateBoundDevicesToClearBlade(service, gcpClient, ctx, devices, errorLogs)

	if len(errorLogs) > 0 {
		if err := generateFailedDevicesCSV(errorLogs); err != nil {
			log.Fatalln(err)
		}
	}

	fmt.Println(string(colorGreen), "\n\n\u2713 Done!", string(colorReset))

}

func validateCBFlags(gcpRegistryRegion string) {

	if Args.cbServiceAccount == "" {
		value, err := readInput("Enter path to ClearBlade service account file. See https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project for more info: ")
		if err != nil {
			log.Fatalln("Error reading service account: ", err)
		}
		Args.cbServiceAccount = value
	}

	// validate that path to service account file exists
	if _, err := os.Stat(Args.cbServiceAccount); errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Could not location service account file %s. Please make sure the path is correct\n", Args.cbServiceAccount)
	}

	err := os.Setenv("CLEARBLADE_CONFIGURATION", Args.cbServiceAccount)
	if err != nil {
		log.Fatalln("Failed to set CLEARBLADE_CONFIGURATION env variable", err.Error())
	}

	if Args.cbRegistryName == "" {
		value, err := readInput("Enter ClearBlade Registry Name: ")
		if err != nil {
			log.Fatalln("Error reading registry name: ", err)
		}
		Args.cbRegistryName = value
	}

	if Args.cbRegistryRegion == "" {
		value, err := readInput("Enter ClearBlade Registry Region (Press enter to skip if you are migrating to the same region): ")
		if err != nil {
			log.Fatalln("Error reading registry region: ", err)
		}

		if value == "" {
			Args.cbRegistryRegion = gcpRegistryRegion
		} else {
			Args.cbRegistryRegion = value
		}

	}

}

func validateGCPIoTCoreFlags() {
	if Args.serviceAccountFile == "" {
		value, err := readInput("Enter GCP Service Account File path (.json): ")
		if err != nil {
			log.Fatalln("Error reading service account file path: ", err)
		}
		Args.serviceAccountFile = value
	}

	if Args.registryName == "" {
		value, err := readInput("Enter Google Registry Name: ")
		if err != nil {
			log.Fatalln("Error reading registry name: ", err)
		}
		Args.registryName = value
	}

	if Args.gcpRegistryRegion == "" {
		value, err := readInput("Enter GCP Registry Region: ")
		if err != nil {
			log.Fatalln("Error reading GCP registry region: ", err)
		}
		Args.gcpRegistryRegion = value
	}

	if Args.devicesCsvFile == "" {
		value, err := readInput("Enter Devices CSV file path (By default all devices from the registry will be migrated. Press enter to skip!): ")
		if err != nil {
			log.Fatalln("Error reading service account file path: ", err)
		}
		Args.devicesCsvFile = value
	}
}

func authenticate() (context.Context, *gcpiotcore.DeviceManagerClient, error) {
	absServiceAccountPath, err := getAbsPath(Args.serviceAccountFile)
	if err != nil {
		errMsg := "Cannot resolve service account filepath: " + err.Error()
		return nil, nil, errors.New(errMsg)
	}

	if !fileExists(absServiceAccountPath) {
		errMsg := "Unable to locate service account credential's filepath: " + absServiceAccountPath
		return nil, nil, errors.New(errMsg)
	}

	ctx := context.Background()
	gcpClient, err := authGCPServiceAccount(ctx, absServiceAccountPath)

	if err != nil {
		log.Fatalln("Unable to authenticate GCP service account: ", err)
	}

	fmt.Println(string(colorGreen), "\n\u2713 GCP Service Account Authenticated!", string(colorReset))

	return ctx, gcpClient, nil
}
