# ClearBlade IoT Core migration tool

Go tool that migrates devices from Google IoT Core registries to the ClearBlade device table.

## Prerequisites

This tool is designed to move devices after enabling the ClearBlade offering in the Google Cloud Marketplace and connecting your project. If you haven't done that already, refer to the folowing documents:

1. [Activate marketplace offering](https://clearblade.atlassian.net/wiki/spaces/IC/pages/2230976570/Google+Cloud+Marketplace+Activation)
2. [Migrating existing registries](https://clearblade.atlassian.net/wiki/spaces/IC/pages/2207449095/Migration+Tutorial)

## Usage

This tool allows multiple CLI flags for starting the migration. See the below chart for available start options as well as their defaults.

| Name | CLI flag | Default | Required |
| ---- | -------- | ------- | -------- |
| Path to ClearBlade Service Account File ([see here for more info](https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project))          | `cbServiceAccount`  | N/A                   | `Yes`  |
| ClearBlade Registry Name                | `cbRegistryName`     | N/A                   | `Yes`  |
| ClearBlade Registry Region              | `cbRegistryRegion`   | `<gcpRegistryRegion>` | `No`   |
| Google IoT Core Registry Name           | `gcpRegistryName`    | N/A                   | `Yes`  |
| Google IoT Core Registry Region         | `gcpRegistryRegion`  | N/A                   | `Yes`  |
| GCP Service account file path           | `gcpServiceAccount`  | N/A                   | `Yes`  |
| Device to migrate CSV file path         | `devicesCsv`         | N/A                   | `No`   |
| Update public keys for existing devices | `updatePublicKeys`   | `true`                | `No`   |
| Store Config Version History            | `configHistory`      | `false`               | `No`   |
| Skip Migrating Latest Config            | `skipConfig`         | `false`               | `No`   |
| Non-Interactive (silent) Mode           | `silentMode`         | `false`               | `No`   |
| Cleanup existing CB registry            | `cleanupCbRegistry`  | `false`               | `No`   |

## Setup

---

### Running the tool

Create a service account by following [this guide](https://clearblade.atlassian.net/wiki/spaces/IC/pages/2240675843/Add+service+accounts+to+a+project).

Install & run the latest binary from https://github.com/ClearBlade/clearblade-iot-core-migration/releases.

`clearblade-iot-core-migration -cbServiceAccount <JSON_FILE_PATH> -cbRegistryName <CB_IOT_CORE_REGISTRY> -cbRegistryRegion <CB_PROJECT_REGION> -gcpServiceAccount <JSON_FILE_PATH> -gcpRegistryName <IOT_CORE_REGISTRY> -gcpRegistryRegion <GCP_PROJECT_REGION>`

You will be prompted to enter a device's CSV file path that will be used to migrate devices specified in the CSV file. You can skip this step by pressing enter; by default, all the registry's devices will be migrated. Alternatively, you can set the `--silentMode` flag to run the tool in non-interactive mode.

**Note: We recommend you use Linux or Darwin binaries. It's unlikely, but something could fail during the migration. A failed_devices CSV file will be created at the end of this migration. Please submit this file to [ClearBlade Support](https://clearblade.atlassian.net/servicedesk/customer/portal/1/group/1/create/20), and we will ensure 100% success.**

**Running this tool in a GCloud instance in the same region as your registry will speed up the migration process.**

**When migrating gateways, the tool checks that bound devices exist, creates those devices if they don't exist, and binds them to the gateways.**

**Rerunning the tool against previously migrated devices and gateways will update them, if needed, and skip them if not. This includes updating gateway to device associations (bindings).**

### Migration tool compilation

The tool was written in Go and therefore requires Go to be installed (https://golang.org/doc/install). To compile the tool for execution, the following steps need to be performed:

1.  Retrieve the migration tool source code.
    - `git clone git@github.com:ClearBlade/clearblade-iot-core-migration.git`
2.  Navigate to the _clearblade-iot-core-migration_ directory.
    - `cd clearblade-iot-core-migration`
3.  Compile the tool for your needed architecture and OS.
    - `GOARCH=arm GOARM=5 GOOS=linux go build`

### Release a new version

To release a new version, the following steps need to be performed:

1.  Commit and push your changes to the master branch.
2.  Add a new tag to the new commit.
    - `git tag -m "Release v1.0.0" v1.0.0 <commit_id>`
3.  Push tags.
    - `git push --tags`
4.  GoReleaser and GitHub actions will take care of releasing new binaries.

## Support

If you have any questions or errors using this tool, please feel free to open tickets on our [IoT Core Support Desk](https://clearblade.atlassian.net/servicedesk/customer/portal/1/group/1/create/20).
