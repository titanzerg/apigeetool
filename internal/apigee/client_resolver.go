package apigee

import (
	"fmt"
	"strings"
)

func resolveClient(existing ManagementClient, host, org, token string) (ManagementClient, error) {
	if existing != nil {
		return existing, nil
	}
	org = strings.TrimSpace(org)
	token = strings.TrimSpace(token)
	host = strings.TrimSpace(host)

	if org == "" || token == "" {
		return nil, fmt.Errorf("Apigee org and token are required")
	}
	return NewClient(host, org, token), nil
}
