# clearblade-iot-core-migration
Go tool that migrates devices from Google IoT Core registries to ClearBlade device table

## Starting the migration tool

This tool allows multiple CLI flags for starting the migration. It is recommended to use a developer account for authentication with this tool. See the below chart for available start options as well as their defaults.

| Name                                         | CLI Flag                  | Default       | Required                                                              |
| -------------------------------------------- | ------------------------- | -------------------------- | -------------------------------------------------------------------- |
| System Key                                   | `systemKey`               | N/A            | `Yes`                                                                  |
| Developer Email                                | `email`            | N/A          | `Yes`                                                                 |
| Developer Token                            | `token`             | N/A                        | `Yes`                                              |
| Google IoT Core Registry Name                  | `registryName`            | N/A                        | `Yes`                                                     |
| Project Region                | `region`              | N/A                        | `Yes` |
| GCP Service account file path               | `gcpServiceAccount`              | N/A                        | `Yes` |
| Device to migrate CSV file path  | `devicesCsv`                | N/A                        | `No`                                                                  |
| Connect to IoT Core sandbox system                        | `sandbox`                      | `false`       | `No`                                                                  |
| Update public keys for existing devices                 | `updatePublicKeys`                       | `true` | `No`                                                                  |

`clearblade-iot-core-migration -systemKey <SYSTEM_KEY> -gcpServiceAccount <JSON_FILE_PATH> -token <DEV_TOKEN> -email <DEV_EMAIL> -registryName <IOT_CORE_REGISTRY> -region <GCP_PROJECT_REGION>`

You will be prompted to enter a devices CSV file path that would be used to migrate devices specified in the CSV file. You can skip this step by pressing enter and by default all devices from the registry will be migrated.  

## Setup

---

The ClearBlade IoT Core Migration tool is dependent upon the ClearBlade Go SDK and its dependent libraries being installed. The tool was written in Go and therefore requires Go to be installed (https://golang.org/doc/install).

### Migration Tool compilation

In order to compile the tool for execution, the following steps need to be performed:

1.  Retrieve the adapter source code
    - `git clone git@github.com:ClearBlade/clearblade-iot-core-migration.git`
2.  Navigate to the _clearblade-iot-core-migration_ directory
    - `cd clearblade-iot-core-migration`
3.  Compile the tool for your needed architecture and OS
    - `GOARCH=arm GOARM=5 GOOS=linux go build`
