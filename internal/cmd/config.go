package cmd

import (
	"fmt"
	"os"
	"strings"
)

type ApigeeConfig struct {
	Org   string
	Token string
	Host  string
}

type SyncArgs struct {
	DBURL          string
	EndpointsTable string
	TargetTable    string
	ProductsTable  string
	SSLRoot        string
	SSLCert        string
	SSLKey         string
}

type GenerateArgs struct {
	InputPath   string
	OutputPath  string
	Name        string
	BasePath    string
	ProxyName   string
	Revision    int
	DownloadDir string
}

func RequireApigeeAuth(cfg ApigeeConfig, action string) error {
	if strings.TrimSpace(cfg.Org) == "" || strings.TrimSpace(cfg.Token) == "" {
		return fmt.Errorf("%s requires Apigee org (-org or APIGEE_ORG) and token (-token or APIGEE_TOKEN)", action)
	}
	return nil
}

func ResolveApigeeConfig(flagOrg, flagToken, flagHost string) ApigeeConfig {
	org := strings.TrimSpace(flagOrg)
	if org == "" {
		org = strings.TrimSpace(os.Getenv("APIGEE_ORG"))
	}

	token := strings.TrimSpace(flagToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("APIGEE_TOKEN"))
	}

	host := strings.TrimSpace(flagHost)
	if host == "" {
		host = "https://apigee.googleapis.com"
	}
	return ApigeeConfig{Org: org, Token: token, Host: host}
}

func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func DefaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
