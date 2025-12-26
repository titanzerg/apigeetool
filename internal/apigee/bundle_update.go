package apigee

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// ReplaceProxyEndpointInBundle swaps a ProxyEndpoint XML file inside a proxy bundle zip.
func ReplaceProxyEndpointInBundle(bundle []byte, endpointFile string, content []byte) ([]byte, string, error) {
	readerAt := bytes.NewReader(bundle)
	zipReader, err := zip.NewReader(readerAt, int64(len(bundle)))
	if err != nil {
		return nil, "", fmt.Errorf("parse bundle zip: %w", err)
	}

	var matchName string
	for _, file := range zipReader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(file.Name), ".xml") {
			continue
		}
		if _, ok := proxyEndpointPrefix(file.Name); !ok {
			continue
		}
		if filepath.Base(file.Name) == endpointFile {
			if matchName != "" {
				return nil, "", fmt.Errorf("multiple ProxyEndpoints named %s found in bundle", endpointFile)
			}
			matchName = file.Name
		}
	}
	if matchName == "" {
		return nil, "", fmt.Errorf("ProxyEndpoint %s not found in bundle", endpointFile)
	}

	var out bytes.Buffer
	zipWriter := zip.NewWriter(&out)
	for _, file := range zipReader.File {
		header := file.FileHeader
		writer, err := zipWriter.CreateHeader(&header)
		if err != nil {
			zipWriter.Close()
			return nil, "", fmt.Errorf("create zip entry %s: %w", file.Name, err)
		}
		if file.FileInfo().IsDir() {
			continue
		}

		if file.Name == matchName {
			if _, err := writer.Write(content); err != nil {
				zipWriter.Close()
				return nil, "", fmt.Errorf("write updated %s: %w", file.Name, err)
			}
			continue
		}

		rc, err := file.Open()
		if err != nil {
			zipWriter.Close()
			return nil, "", fmt.Errorf("open %s: %w", file.Name, err)
		}
		if _, err := io.Copy(writer, rc); err != nil {
			rc.Close()
			zipWriter.Close()
			return nil, "", fmt.Errorf("copy %s: %w", file.Name, err)
		}
		rc.Close()
	}
	if err := zipWriter.Close(); err != nil {
		return nil, "", fmt.Errorf("finalize zip: %w", err)
	}
	return out.Bytes(), matchName, nil
}
