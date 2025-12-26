package apigee

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
)

// ImportProxyBundle uploads a proxy bundle and returns the new revision number.
func (c *Client) ImportProxyBundle(proxy string, bundle []byte) (int, error) {
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/apis?action=import&name=%s",
		c.host,
		url.PathEscape(c.org),
		url.QueryEscape(proxy),
	)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", proxy+".zip")
	if err != nil {
		return 0, err
	}
	if _, err := part.Write(bundle); err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("import proxy failed: %s", extractError(resp))
	}

	var payload struct {
		Revision string `json:"revision"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode import response: %w", err)
	}
	if payload.Revision == "" {
		return 0, fmt.Errorf("import response missing revision")
	}
	rev, err := strconv.Atoi(payload.Revision)
	if err != nil {
		return 0, fmt.Errorf("invalid revision %q", payload.Revision)
	}
	return rev, nil
}

// DeployRevision deploys a proxy revision to the specified environment.
func (c *Client) DeployRevision(proxy, env string, revision int, override bool) error {
	query := ""
	if override {
		query = "?override=true"
	}
	endpoint := fmt.Sprintf(
		"%s/v1/organizations/%s/environments/%s/apis/%s/revisions/%d/deployments%s",
		c.host,
		url.PathEscape(c.org),
		url.PathEscape(env),
		url.PathEscape(proxy),
		revision,
		query,
	)
	resp, err := c.doRequestAny(http.MethodPost, endpoint, "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("deploy failed: %s", extractError(resp))
	}
	return nil
}
