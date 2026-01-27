package proxyxml

import "testing"

func TestParseTargetEndpointDetails(t *testing.T) {
	data := []byte(`
<TargetEndpoint name="default">
  <HTTPTargetConnection>
    <URL>https://example.com</URL>
    <LoadBalancer>
      <Server name="lb1"/>
      <Server><Name>lb2</Name></Server>
    </LoadBalancer>
    <Properties>
      <Property name="success.codes">1xx,2xx</Property>
      <Property name="custom"> abc </Property>
    </Properties>
  </HTTPTargetConnection>
</TargetEndpoint>
`)

	details, err := ParseTargetEndpointDetails(data)
	if err != nil {
		t.Fatalf("ParseTargetEndpointDetails error: %v", err)
	}
	if details.Name != "default" {
		t.Fatalf("Name = %q, want %q", details.Name, "default")
	}
	if details.URL != "https://example.com" {
		t.Fatalf("URL = %q, want %q", details.URL, "https://example.com")
	}
	if len(details.LoadBalancer) != 2 || details.LoadBalancer[0] != "lb1" || details.LoadBalancer[1] != "lb2" {
		t.Fatalf("LoadBalancer = %#v, want [lb1 lb2]", details.LoadBalancer)
	}
	if got := details.Properties["custom"]; got != "abc" {
		t.Fatalf("Properties[custom] = %q, want %q", got, "abc")
	}
	if got := details.SuccessCodes; got != "1xx,2xx" {
		t.Fatalf("SuccessCodes = %q, want %q", got, "1xx,2xx")
	}
}
