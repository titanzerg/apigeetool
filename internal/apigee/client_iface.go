package apigee

// ManagementClient exposes the subset of Apigee Management API operations used by the CLI.
type ManagementClient interface {
	ListAPIs() ([]string, error)
	LatestRevision(proxy string) (int, error)
	FetchProxyBundle(proxy string, revision int) ([]byte, error)
	ListEnvironments() ([]string, error)
	EnvironmentsForRevision(proxy string, revision int) ([]string, error)
	ListTargetServers(env string) ([]string, error)
	FetchTargetServer(env, name string) (TargetServerRecord, error)
	ListAPIProducts() ([]string, error)
	FetchAPIProduct(name string) (apiProductDetail, error)
	ListOrganizationApps(startKey string) (orgAppsPage, error)
	FetchAPIProductApps(name string) ([]string, error)
}
