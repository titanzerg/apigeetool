package cmd

import (
	"fmt"
	"strings"

	"apigee/internal/apigee"
)

func syncProxyScanProgressPrinter() func(apigee.ProxyScanProgress) {
	return func(p apigee.ProxyScanProgress) {
		if p.Err != nil {
			fmt.Printf("[%d/%d] %s (rev %d) error: %v\n", p.Index, p.Total, p.Proxy, p.Revision, p.Err)
			return
		}
		basePaths := noneLabel
		if len(p.BasePaths) > 0 {
			basePaths = strings.Join(p.BasePaths, ", ")
		}
		envs := noneLabel
		if len(p.Envs) > 0 {
			envs = strings.Join(p.Envs, ", ")
		}
		if p.EnvError != "" {
			envs = fmt.Sprintf("%s (env error: %s)", envs, p.EnvError)
		}
		fmt.Printf("[%d/%d] %s (rev %d) basepaths: %s envs: %s\n", p.Index, p.Total, p.Proxy, p.Revision, basePaths, envs)
	}
}

func targetServerProgressPrinter() func(apigee.TargetServerProgress) {
	return func(p apigee.TargetServerProgress) {
		if p.Total == 0 {
			return
		}
		url := strings.TrimSpace(p.URL)
		if url == "" {
			url = "<unknown>"
		}
		if p.Err != nil {
			fmt.Printf("[%d/%d] target %s/%s url: %s error: %v\n", p.Index, p.Total, p.Environment, p.Name, url, p.Err)
			return
		}
		fmt.Printf("[%d/%d] target %s/%s url: %s\n", p.Index, p.Total, p.Environment, p.Name, url)
	}
}

func apiProductProgressPrinter() func(apigee.APIProductProgress) {
	return func(p apigee.APIProductProgress) {
		envs := noneLabel
		if len(p.Environments) > 0 {
			envs = strings.Join(p.Environments, ", ")
		}
		proxies := noneLabel
		if len(p.Proxies) > 0 {
			proxies = strings.Join(p.Proxies, ", ")
		}
		if p.Err != nil {
			fmt.Printf("[%d/%d] product %s envs: %s proxies: %s apps: %d error: %v\n", p.Index, p.Total, p.Name, envs, proxies, p.Apps, p.Err)
			return
		}
		fmt.Printf("[%d/%d] product %s envs: %s proxies: %s apps: %d\n", p.Index, p.Total, p.Name, envs, proxies, p.Apps)
	}
}

func appProgressPrinter() func(apigee.AppProgress) {
	return func(p apigee.AppProgress) {
		label := fmt.Sprintf("[%d/?]", p.Index)
		if p.Total > 0 {
			label = fmt.Sprintf("[%d/%d]", p.Index, p.Total)
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = "<unnamed>"
		}
		appID := strings.TrimSpace(p.AppID)
		if appID == "" {
			appID = "<unknown>"
		}
		if p.Err != nil {
			fmt.Printf("%s app %s id: %s error: %v\n", label, name, appID, p.Err)
			return
		}
		fmt.Printf("%s app %s id: %s credentials: %d\n", label, name, appID, p.Credentials)
	}
}
