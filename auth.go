package main

import (
	gcpiotcore "cloud.google.com/go/iot/apiv1"
	"context"
	cb "github.com/clearblade/Go-SDK"
	"google.golang.org/api/option"
	"strings"
)

func authGCPServiceAccount(ctx context.Context, absServiceAccountPath string) (*gcpiotcore.DeviceManagerClient, error) {
	c, err := gcpiotcore.NewDeviceManagerClient(ctx, option.WithCredentialsFile(absServiceAccountPath))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func authClearBladeAccount() (*cb.DevClient, error) {
	messagingURL := strings.Split(Args.platformURL, "//")[1]
	client := cb.NewDevClientWithTokenAndAddrs(Args.platformURL, messagingURL, Args.token, Args.email)
	err := client.CheckAuth()
	if err != nil {
		return nil, err
	}
	return client, nil
}
