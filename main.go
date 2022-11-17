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
	platformURL      string
	token            string
	systemKey        string
	cbRegistryRegion string
	platformURLREST  string
	// GCP IoT Core specific flags
	serviceAccountFile string
	registryName       string
	gcpRegistryRegion  string

	// Optional flags
	devicesCsvFile   string
	configHistory    bool
	sandbox          bool
	updatePublicKeys bool
}

func initMigrationFlags() {
	flag.StringVar(&Args.token, "cbToken", "", "ClearBlade User Token (Required)")
	flag.StringVar(&Args.systemKey, "cbSystemKey", "", "ClearBlade System Key (Required)")
	flag.StringVar(&Args.cbRegistryRegion, "cbRegistryRegion", "", "ClearBlade Registry Region (Optional)")

	flag.StringVar(&Args.serviceAccountFile, "gcpServiceAccount", "", "Service account file path (Required)")
	flag.StringVar(&Args.registryName, "gcpRegistryName", "", "Google Registry Name (Required)")
	flag.StringVar(&Args.gcpRegistryRegion, "gcpRegistryRegion", "", "Google Registry Region (Required)")

	// Optional
	flag.StringVar(&Args.devicesCsvFile, "devicesCsv", "", "Devices CSV file path")
	flag.BoolVar(&Args.configHistory, "configHistory", false, "Store Config History. Default is false")
	flag.BoolVar(&Args.sandbox, "sandbox", false, "Connect to IoT Core sandbox system. Default is false")
	flag.BoolVar(&Args.updatePublicKeys, "updatePublicKeys", true, "Replace existing keys of migrated devices. Default is true")
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
	validateCBFlags()

	fmt.Println(string(colorGreen), "\n\u2713 All Flags validated!", string(colorReset))

	// Authenticate GCP service user and Clearblade User account
	ctx, gcpClient, err := authenticate()

	if err != nil {
		log.Fatalln(err)
	}

	exists := registryExistsInClearBlade(Args.registryName)
	if exists {
		log.Println(Args.registryName, " is already in Clearblade project.")
	} else {
		log.Fatalln(Args.registryName, " registry is not present in the Clearblade project. "+
			"Please create the registry in clearblade then retry")
		os.Exit(1)
		//TODO: next, create the registry through an API call
	}
	// Fetch devices from the given registry
	devices, deviceConfigs := fetchDevicesFromGoogleIotCore(ctx, gcpClient)

	fmt.Println(string(colorCyan), "\nPreparing Device Migration\n", string(colorReset))

	// Add fetched devices to ClearBlade Device table
	addDevicesToClearBlade(devices, deviceConfigs)

	fmt.Println(string(colorGreen), "\n\u2713 Done!", string(colorReset))

}

func validateCBFlags() {
	if Args.systemKey == "" {
		value, err := readInput("Enter ClearBlade Platform System Key: ")
		if err != nil {
			log.Fatalln("Error reading system key: ", err)
		}
		Args.systemKey = value
	}

	if Args.token == "" {
		value, err := readInput("Enter ClearBlade User Token: ")
		if err != nil {
			log.Fatalln("Error reading user token: ", err)
		}
		Args.token = value
	}

	if Args.cbRegistryRegion == "" {
		value, err := readInput("Enter ClearBlade Registry Region (Press enter to skip if you are migrating to the same region): ")
		if err != nil {
			log.Fatalln("Error reading registry region: ", err)
		}

		if value == "" {
			Args.platformURL = getURI(Args.gcpRegistryRegion)
		} else {
			Args.platformURL = getURI(value)
		}
	} else {
		Args.platformURL = getURI(Args.cbRegistryRegion)
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
