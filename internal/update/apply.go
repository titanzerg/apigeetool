package update

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// ConfirmApply prompts the user to confirm whether the downloaded ProxyEndpoint should be updated.
func ConfirmApply() (bool, error) {
	return confirmApply(os.Stdin, os.Stdout)
}

// ReplaceProxyEndpoint overwrites the target file with the generated ProxyEndpoint contents.
func ReplaceProxyEndpoint(generatedPath, targetPath string) error {
	data, err := os.ReadFile(generatedPath)
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0o644)
}

func confirmApply(in io.Reader, out io.Writer) (bool, error) {
	reader := bufio.NewReader(in)
	for {
		if _, err := fmt.Fprint(out, "Apply generated changes to the downloaded ProxyEndpoint? [y/N]: "); err != nil {
			return false, err
		}
		resp, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}
		resp = strings.TrimSpace(strings.ToLower(resp))
		if resp == "" {
			return false, nil
		}
		if resp == "y" || resp == "yes" {
			return true, nil
		}
		if resp == "n" || resp == "no" {
			return false, nil
		}
		if _, err := fmt.Fprintln(out, "Please answer y or n."); err != nil {
			return false, err
		}
	}
}
