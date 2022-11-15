# ClearBlade IoT Core Migration Tool
Go tool that migrates devices from Google IoT Core registries to ClearBlade device table

## Prerequisites

This tools is designed to move devices after you have already enabled the offering in the Google Cloud marketplace and have connected your project. If you haven't done that already, refer the folowing documents to -  

1. [Activate Marketplace offering](https://clearblade.atlassian.net/wiki/spaces/IC/pages/2230976570/Google+Cloud+Marketplace+Activation)
2. [Migrating existing registries](https://clearblade.atlassian.net/wiki/spaces/IC/pages/2207449095/Migration+Tutorial)


## Usage

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

  
## Setup

---

### Running the tool

Install & run the latest binary from https://github.com/ClearBlade/clearblade-iot-core-migration/releases.

`clearblade-iot-core-migration -cbSystemKey <SYSTEM_KEY> -gcpServiceAccount <JSON_FILE_PATH> -cbToken <DEV_TOKEN> -gcpRegistryName <IOT_CORE_REGISTRY> -gcpRegistryRegion <GCP_PROJECT_REGION>`

You will be prompted to enter a devices CSV file path that would be used to migrate devices specified in the CSV file. You can skip this step by pressing enter and by default all devices from the registry will be migrated.

**Note - We recommend you to use linux or darwin binaries. It's unlikely but possible something could fail during the migration. A failed_devices CSV file will be created at the end of this migration.  Please submit this file to the [ClearBlade Support](https://clearblade.atlassian.net/servicedesk/customer/portal/1/group/1/create/20) and we will ensure 100% success** 

**Running this tool in a GCloud instance in the same region as your registry will speed up the migration process.**


### Migration Tool compilation

The tool was written in Go and therefore requires Go to be installed (https://golang.org/doc/install). In order to compile the tool for execution, the following steps need to be performed:

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

## Support


If you have any questions or errors using this tool, please feel free to open tickets on our [IoT Core Support Desk](https://clearblade.atlassian.net/servicedesk/customer/portal/1/group/1/create/20)