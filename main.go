package main

import (
	gcpiotcore "cloud.google.com/go/iot/apiv1"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
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
	platformURL string
	email       string
	token       string
	systemKey   string

	// GCP IoT Core specific flags
	serviceAccountFile string
	registryName       string
	region             string

	// Optional flags
	devicesCsvFile   string
	stateHistory     bool
	configHistory    bool
	sandbox          bool
	updatePublicKeys bool
}

func initMigrationFlags() {
	flag.StringVar(&Args.email, "email", "", "ClearBlade User Email (Required)")
	flag.StringVar(&Args.token, "token", "", "ClearBlade User Token (Required)")
	flag.StringVar(&Args.systemKey, "systemKey", "", "ClearBlade System Key (Required)")

	flag.StringVar(&Args.serviceAccountFile, "gcpServiceAccount", "", "Service account file path (Required)")
	flag.StringVar(&Args.registryName, "registryName", "", "Registry Name (Required)")
	flag.StringVar(&Args.region, "region", "", "Project Region (Required)")

	// Optional
	flag.StringVar(&Args.devicesCsvFile, "devicesCsv", "", "Devices CSV file path")
	flag.BoolVar(&Args.stateHistory, "stateHistory", false, "Store State History. Default is false")
	flag.BoolVar(&Args.configHistory, "configHistory", false, "Store Config History. Default is false")
	flag.BoolVar(&Args.sandbox, "sandbox", false, "Connect to IoT Core sandbox system. Default is false")
	flag.BoolVar(&Args.updatePublicKeys, "updatePublicKeys", true, "Replace existing keys of migrated devices. Default is true")
}

func main() {

	// Init & Parse migration Flags
	initMigrationFlags()
	flag.Parse()

	fmt.Println(string(colorCyan), "\n\n================= Starting Device Migration =================\n", string(colorReset))

	// Validate if all required CB flags are provided
	validateCBFlags()

	// Validate if all required Google IOT Core flags are provided
	validateGCPIoTCoreFlags()

	fmt.Println(string(colorGreen), "\n\u2713 All Flags validated!", string(colorReset))

	// Authenticate GCP service user and Clearblade User account
	ctx, gcpClient, err := authenticate()

	if err != nil {
		log.Fatalln(err)
	}

	// Fetch devices from the given registry
	devices := fetchDevicesFromGoogleIotCore(ctx, gcpClient)

	fmt.Println(string(colorCyan), "\nPreparing Device Migration\n", string(colorReset))

	// Add fetched devices to ClearBlade Device table
	addDevicesToClearBlade(devices)

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

	if Args.email == "" {
		value, err := readInput("Enter ClearBlade User Email: ")
		if err != nil {
			log.Fatalln("Error reading user email: ", err)
		}
		Args.email = value
	}

	if Args.token == "" {
		value, err := readInput("Enter ClearBlade User Token: ")
		if err != nil {
			log.Fatalln("Error reading user token: ", err)
		}
		Args.token = value
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

	if Args.devicesCsvFile == "" {
		value, err := readInput("Enter Devices CSV file path (By default all devices from the registry will be migrated. Press enter to skip!): ")
		if err != nil {
			log.Fatalln("Error reading service account file path: ", err)
		}
		Args.devicesCsvFile = value
	}

	if Args.registryName == "" {
		value, err := readInput("Enter Registry Name: ")
		if err != nil {
			log.Fatalln("Error reading registry name: ", err)
		}
		Args.registryName = value
	}

	if Args.region == "" {
		value, err := readInput("Enter Project Region: ")
		if err != nil {
			log.Fatalln("Error reading project region: ", err)
		}
		Args.region = value
	}

	Args.platformURL = getURI(Args.region)
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
