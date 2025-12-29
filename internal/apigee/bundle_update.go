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

	matchName, err := findProxyEndpointInBundle(zipReader, endpointFile)
	if err != nil {
		return nil, "", err
	}

	var out bytes.Buffer
	zipWriter := zip.NewWriter(&out)
	for _, file := range zipReader.File {
		if err := copyZipEntry(zipWriter, file, matchName, content); err != nil {
			zipWriter.Close()
			return nil, "", err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, "", fmt.Errorf("finalize zip: %w", err)
	}
	return out.Bytes(), matchName, nil
}

func findProxyEndpointInBundle(reader *zip.Reader, endpointFile string) (string, error) {
	var matchName string
	for _, file := range reader.File {
		if !isProxyEndpointFile(file) {
			continue
		}
		if filepath.Base(file.Name) != endpointFile {
			continue
		}
		if matchName != "" {
			return "", fmt.Errorf("multiple ProxyEndpoints named %s found in bundle", endpointFile)
		}
		matchName = file.Name
	}
	if matchName == "" {
		return "", fmt.Errorf("ProxyEndpoint %s not found in bundle", endpointFile)
	}
	return matchName, nil
}

func isProxyEndpointFile(file *zip.File) bool {
	if file.FileInfo().IsDir() {
		return false
	}
	if !strings.HasSuffix(strings.ToLower(file.Name), ".xml") {
		return false
	}
	_, ok := proxyEndpointPrefix(file.Name)
	return ok
}

func copyZipEntry(writer *zip.Writer, file *zip.File, matchName string, content []byte) error {
	header := file.FileHeader
	entry, err := writer.CreateHeader(&header)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", file.Name, err)
	}
	if file.FileInfo().IsDir() {
		return nil
	}
	if file.Name == matchName {
		if _, err := entry.Write(content); err != nil {
			return fmt.Errorf("write updated %s: %w", file.Name, err)
		}
		return nil
	}
	rc, err := file.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", file.Name, err)
	}
	if _, err := io.Copy(entry, rc); err != nil {
		rc.Close()
		return fmt.Errorf("copy %s: %w", file.Name, err)
	}
	return rc.Close()
}
