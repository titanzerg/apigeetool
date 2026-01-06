package apigee

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"apigee/internal/util"
)

// CollectAppsOptions controls the behavior of CollectApps.
type CollectAppsOptions struct {
	Host     string
	Org      string
	Token    string
	Client   ManagementClient
	Progress func(AppProgress)
}

// AppRecord holds the minimal set of app data to sync.
type AppRecord struct {
	Name         string
	AppID        string
	Owner        string
	RegisteredAt *time.Time
	Notes        string
}

// AppProgress describes the progress of fetching an app.
type AppProgress struct {
	Index       int
	Total       int
	Name        string
	AppID       string
	Credentials int
	Err         error
}

// AppCredentialRecord stores consumer keys for an app.
type AppCredentialRecord struct {
	AppID     string
	Key       string
	Secret    string
	ExpiresAt *time.Time
	Products  []string
}

// CollectApps fetches all apps with their credentials.
func CollectApps(opts CollectAppsOptions) ([]AppRecord, []AppCredentialRecord, error) {
	client, err := resolveClient(opts.Client, opts.Host, opts.Org, opts.Token)
	if err != nil {
		return nil, nil, err
	}

	var apps []AppRecord
	var creds []AppCredentialRecord
	appIDs := make(map[string]struct{})
	credKeys := make(map[string]struct{})

	startKey := ""
	index := 0
	for {
		page, err := client.ListOrganizationApps(startKey)
		if err != nil {
			reportAppProgress(opts.Progress, AppProgress{
				Index: index,
				Total: 0,
				Err:   err,
			})
			return nil, nil, fmt.Errorf("list organization apps: %w", err)
		}
		for _, app := range page.Apps {
			name := strings.TrimSpace(firstNonEmpty(app.AppName, app.Name, app.AppID))
			appID := strings.TrimSpace(app.AppID)
			if appID == "" {
				appID = name
			}
			if appID == "" {
				continue
			}
			added := false
			if _, ok := appIDs[appID]; !ok {
				owner := strings.TrimSpace(firstNonEmpty(app.AppOwner, app.DeveloperEmail, app.DeveloperID))
				notes := strings.TrimSpace(findAppAttribute(app.Attributes, "notes"))
				apps = append(apps, AppRecord{
					Name:         name,
					AppID:        appID,
					Owner:        owner,
					RegisteredAt: app.CreatedAt.Time(),
					Notes:        notes,
				})
				appIDs[appID] = struct{}{}
				index++
				added = true
			}
			if !added {
				continue
			}
			credCount := 0
			for _, cred := range app.Credentials {
				key := strings.TrimSpace(cred.ConsumerKey)
				if key == "" {
					continue
				}
				secret := strings.TrimSpace(cred.ConsumerSecret)
				credKey := appID + "|" + key
				if _, ok := credKeys[credKey]; ok {
					continue
				}
				credKeys[credKey] = struct{}{}
				creds = append(creds, AppCredentialRecord{
					AppID:     appID,
					Key:       key,
					Secret:    secret,
					ExpiresAt: cred.ExpiresAt.Time(),
					Products:  util.UniqueSortedStrings(cred.APIProducts),
				})
				credCount++
			}
			reportAppProgress(opts.Progress, AppProgress{
				Index:       index,
				Total:       0,
				Name:        name,
				AppID:       appID,
				Credentials: credCount,
			})
		}
		if next := strings.TrimSpace(page.NextStartKey); next == "" {
			break
		} else {
			startKey = next
		}
	}

	return apps, creds, nil
}

func reportAppProgress(fn func(AppProgress), progress AppProgress) {
	if fn == nil {
		return
	}
	fn(progress)
}

type epochMillis struct {
	value int64
	valid bool
}

func (e *epochMillis) UnmarshalJSON(data []byte) error {
	data = bytesTrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' && data[len(data)-1] == '"' {
		data = data[1 : len(data)-1]
	}
	if len(data) == 0 {
		return nil
	}
	value, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return nil
	}
	e.value = value
	e.valid = true
	return nil
}

func (e epochMillis) Time() *time.Time {
	if !e.valid || e.value <= 0 {
		return nil
	}
	t := time.UnixMilli(e.value).UTC()
	return &t
}

func bytesTrimSpace(in []byte) []byte {
	return []byte(strings.TrimSpace(string(in)))
}

func findAppAttribute(attrs []nameValue, key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return ""
	}
	for _, attr := range attrs {
		if strings.ToLower(strings.TrimSpace(attr.Name)) == key {
			return attr.Value
		}
	}
	return ""
}

var _ json.Unmarshaler = (*epochMillis)(nil)
