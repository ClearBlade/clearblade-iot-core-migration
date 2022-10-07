package main

import (
	gcpiotcore "cloud.google.com/go/iot/apiv1"
	"context"
	"google.golang.org/api/option"
)

func authGCPServiceAccount(ctx context.Context, absServiceAccountPath string) (*gcpiotcore.DeviceManagerClient, error) {
	c, err := gcpiotcore.NewDeviceManagerClient(ctx, option.WithCredentialsFile(absServiceAccountPath))
	if err != nil {
		return nil, err
	}
	return c, nil
}
