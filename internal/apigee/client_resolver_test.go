package apigee

import "testing"

func TestResolveClientReturnsExisting(t *testing.T) {
	existing := &Client{}
	got, err := resolveClient(existing, "", "", "")
	if err != nil {
		t.Fatalf("resolveClient existing: unexpected error %v", err)
	}
	if got != existing {
		t.Fatalf("resolveClient should return existing client instance")
	}
}

func TestResolveClientRequiresOrgAndToken(t *testing.T) {
	if _, err := resolveClient(nil, "https://example.com", "", ""); err == nil {
		t.Fatalf("resolveClient should error when org/token are missing")
	}
}

func TestResolveClientBuildsNewClient(t *testing.T) {
	got, err := resolveClient(nil, "", "my-org", "secret-token")
	if err != nil {
		t.Fatalf("resolveClient: unexpected error %v", err)
	}
	client, ok := got.(*Client)
	if !ok {
		t.Fatalf("resolveClient: expected *Client, got %T", got)
	}
	if client.host != "https://apigee.googleapis.com" {
		t.Fatalf("client host = %q, want default https://apigee.googleapis.com", client.host)
	}
	if client.org != "my-org" {
		t.Fatalf("client org = %q, want my-org", client.org)
	}
	if client.token != "secret-token" {
		t.Fatalf("client token = %q, want secret-token", client.token)
	}
}
