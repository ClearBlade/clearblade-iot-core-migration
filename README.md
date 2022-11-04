# ClearBlade IoT Core Migration Tool
Go tool that migrates devices from Google IoT Core registries to ClearBlade device table

## Starting the migration tool

This tool allows multiple CLI flags for starting the migration. See the below chart for available start options as well as their defaults.

| Name                                         | CLI Flag                  | Default       | Required                                                              |
| -------------------------------------------- | ------------------------- | -------------------------- | -------------------------------------------------------------------- |
| ClearBlade System Key                                   | `cbSystemKey`               | N/A            | `Yes`                                                                  |
| ClearBlade User Token                            | `cbToken`             | N/A                        | `Yes`                                              |
| ClearBlade Registry Region                            | `cbRegistryRegion`             | N/A                        | `No`                                              |
| Google IoT Core Registry Name                  | `gcpRegistryName`            | N/A                        | `Yes`                                                     |
| Google IoT Core Registry Region                | `gcpRegistryRegion`              | N/A                        | `Yes` |
| GCP Service account file path               | `gcpServiceAccount`              | N/A                        | `Yes` |
| Device to migrate CSV file path  | `devicesCsv`                | N/A                        | `No`                                                                  |
| Connect to IoT Core sandbox system                        | `sandbox`                      | `false`       | `No`                                                                  |
| Update public keys for existing devices                 | `updatePublicKeys`                       | `true` | `No`                                                                  |
| Store Config Version History                 | `configHistory`                       | `false` | `No`                                                                  |

`clearblade-iot-core-migration -cbSystemKey <SYSTEM_KEY> -gcpServiceAccount <JSON_FILE_PATH> -cbToken <DEV_TOKEN> -gcpRegistryName <IOT_CORE_REGISTRY> -gcpRegistryRegion <GCP_PROJECT_REGION>`

You will be prompted to enter a devices CSV file path that would be used to migrate devices specified in the CSV file. You can skip this step by pressing enter and by default all devices from the registry will be migrated.  

## Setup

---

### Running the tool

Install & run the latest binary from https://github.com/ClearBlade/clearblade-iot-core-migration/releases.

**Note - We recommend you to use linux or darwin binaries.** 

The tool was written in Go and therefore requires Go to be installed (https://golang.org/doc/install).

### Migration Tool compilation

In order to compile the tool for execution, the following steps need to be performed:

1.  Retrieve the adapter source code
    - `git clone git@github.com:ClearBlade/clearblade-iot-core-migration.git`
2.  Navigate to the _clearblade-iot-core-migration_ directory
    - `cd clearblade-iot-core-migration`
3.  Compile the tool for your needed architecture and OS
    - `GOARCH=arm GOARM=5 GOOS=linux go build`


### Release a new version

In order to release a new version, the following steps need to be performed:

1.  Commit and push your changes to the master branch
2.  Add a new tag to the new commit
    - `git tag -m "Release v1.0.0" v1.0.0 <commit_id>`
3.  Push tags
    - `git push --tags`
4. Goreleaser and github actions will take care of releasing new binaries
