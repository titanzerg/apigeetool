package apigee

import "testing"

func TestCollectAppsByProductCaseInsensitive(t *testing.T) {
	client := &mockAppLister{
		pages: []orgAppsPage{
			{
				Apps: []orgApp{
					{Name: "app1", APIProducts: apiProductNames{"Inventory-Management"}},
					{Name: "app2", APIProducts: apiProductNames{"inventory-management"}},
				},
			},
		},
	}
	appsByProduct, err := collectAppsByProduct(client, false)
	if err != nil {
		t.Fatalf("collectAppsByProduct returned error: %v", err)
	}

	apps := appsByProduct["inventory-management"]
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d (%v)", len(apps), apps)
	}
}

type mockAppLister struct {
	pages []orgAppsPage
	call  int
}

func (m *mockAppLister) ListOrganizationApps(startKey string) (orgAppsPage, error) {
	if m.call >= len(m.pages) {
		return orgAppsPage{}, nil
	}
	page := m.pages[m.call]
	m.call++
	return page, nil
}

// Unused interface methods for this mock.
func (m *mockAppLister) ListAPIProducts() ([]string, error) { return nil, nil }
func (m *mockAppLister) FetchAPIProduct(name string) (apiProductDetail, error) {
	return apiProductDetail{}, nil
}
func (m *mockAppLister) ListTargetServers(env string) ([]string, error) { return nil, nil }
func (m *mockAppLister) FetchTargetServer(env, name string) (TargetServerRecord, error) {
	return TargetServerRecord{}, nil
}
func (m *mockAppLister) ListAPIs() ([]string, error)              { return nil, nil }
func (m *mockAppLister) LatestRevision(proxy string) (int, error) { return 0, nil }
func (m *mockAppLister) FetchProxyBundle(proxy string, revision int) ([]byte, error) {
	return nil, nil
}
func (m *mockAppLister) ListEnvironments() ([]string, error) { return nil, nil }
func (m *mockAppLister) EnvironmentsForRevision(proxy string, revision int) ([]string, error) {
	return nil, nil
}
func (m *mockAppLister) FetchAPIProductApps(name string) ([]string, error) { return nil, nil }
