/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// npd-prober is a lightweight binary that performs HTTP or TCP probes
// and returns NPD-compatible exit codes (0 for success, 1 for failure,
// and 2 for unknown). It is designed to be used as a custom plugin for
// node-problem-detector (NPD), allowing operatorsto reuse kubelet-style
// probe semantics for node-level readiness checks.

// Exit codes follow NPD convention:
//
//	0 = OK (healthy)
//	1 = NonOK (unhealthy)
//	2 = Unknown (configuration error)

package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"k8s.io/klog/v2"
)

// NPD custom plugin exit codes.
const (
	exitOK      = 0
	exitNonOK   = 1
	exitUnknown = 2
)

func main() {
	probeType := flag.String("probe-type", "", "Probe type: http or tcp")
	httpURL := flag.String("http-url", "", "URL for HTTP probe (required when probe-type=http)")
	tcpAddr := flag.String("tcp-addr", "", "Address (host:port) for TCP probe (required when probe-type=tcp)")
	timeout := flag.Duration("timeout", 5*time.Second, "Probe timeout")
	allowNonLocalRedirects := flag.Bool("allow-non-local-redirects", false,
		"Allow HTTP redirects to non-local hosts (default false, matching kubelet behavior)")

	klog.InitFlags(nil)
	flag.Parse()

	code, msg := run(*probeType, *httpURL, *tcpAddr, *timeout, *allowNonLocalRedirects)
	// Print to stdout for NPD capture (NPD reads stdout, not stderr where klog writes).
	fmt.Println(msg)
	if code == exitOK {
		klog.InfoS("Probe completed", "result", msg, "exitCode", code)
	} else {
		klog.ErrorS(nil, "Probe completed", "result", msg, "exitCode", code)
	}
	os.Exit(code)
}

// run executes the probe and returns an exit code and message.
func run(probeType, httpURL, tcpAddr string, timeout time.Duration, allowNonLocalRedirects bool) (int, string) {
	switch probeType {
	case "http":
		return probeHTTP(httpURL, timeout, allowNonLocalRedirects)
	case "tcp":
		return probeTCP(tcpAddr, timeout)
	default:
		return exitUnknown, "unknown or missing --probe-type (must be http or tcp)"
	}
}

// redirectChecker returns a CheckRedirect function for http.Client.
// When allowNonLocal is false, redirects to a different host than the
// original request are blocked by returning http.ErrUseLastResponse,
// matching kubelet's default HTTP probe behavior.
func redirectChecker(allowNonLocal bool) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if !allowNonLocal && len(via) > 0 {
			if req.URL.Hostname() != via[0].URL.Hostname() {
				klog.InfoS("Blocked non-local redirect",
					"from", via[0].URL.String(),
					"to", req.URL.String())
				return http.ErrUseLastResponse
			}
		}
		return nil
	}
}

// probeHTTP performs an HTTP GET and checks the response status code.
// Status 200-399 is healthy (matching kubelet HTTP probe semantics).
func probeHTTP(url string, timeout time.Duration, allowNonLocalRedirects bool) (int, string) {
	if url == "" {
		return exitUnknown, "missing --http-url for http probe"
	}

	client := &http.Client{
		Timeout:       timeout,
		CheckRedirect: redirectChecker(allowNonLocalRedirects),
	}
	klog.InfoS("Starting HTTP probe", "url", url)

	resp, err := client.Get(url) //nolint:noctx // simple probe binary, context not needed
	if err != nil {
		return exitNonOK, fmt.Sprintf("http probe failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusBadRequest {
		return exitOK, fmt.Sprintf("http probe healthy: status %d", resp.StatusCode)
	}
	return exitNonOK, fmt.Sprintf("http probe unhealthy: status %d", resp.StatusCode)
}

// probeTCP attempts a TCP connection. Success means healthy.
func probeTCP(addr string, timeout time.Duration) (int, string) {
	if addr == "" {
		return exitUnknown, "missing --tcp-addr for tcp probe"
	}

	klog.InfoS("Starting TCP probe", "addr", addr)

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return exitNonOK, fmt.Sprintf("tcp probe failed: %v", err)
	}
	conn.Close()
	return exitOK, fmt.Sprintf("tcp probe healthy: connected to %s", addr)
}
