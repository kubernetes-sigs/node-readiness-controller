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

package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProbeHTTP(t *testing.T) {
	tests := []struct {
		name         string
		handler      http.HandlerFunc
		url          string // override URL (empty = use test server)
		wantCode     int
		wantContains string
	}{
		{
			name:         "200 OK",
			handler:      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
			wantCode:     exitOK,
			wantContains: "healthy",
		},
		{
			name:         "301 redirect (still healthy)",
			handler:      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusMovedPermanently) },
			wantCode:     exitOK,
			wantContains: "healthy",
		},
		{
			name:         "404 not found",
			handler:      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
			wantCode:     exitNonOK,
			wantContains: "unhealthy",
		},
		{
			name:         "500 internal server error",
			handler:      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			wantCode:     exitNonOK,
			wantContains: "unhealthy",
		},
		{
			name:         "unreachable server",
			url:          "http://127.0.0.1:1", // port 1 is unlikely to be listening
			wantCode:     exitNonOK,
			wantContains: "failed",
		},
		{
			name:         "missing URL",
			url:          "", // explicitly empty
			wantCode:     exitUnknown,
			wantContains: "missing --http-url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.url
			if tt.handler != nil && url == "" {
				// Disable redirect following so we can test 3xx codes directly.
				ts := httptest.NewServer(tt.handler)
				defer ts.Close()
				url = ts.URL
			}

			code, msg := probeHTTP(url, 2*time.Second, false)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (msg: %s)", code, tt.wantCode, msg)
			}
			if !strings.Contains(msg, tt.wantContains) {
				t.Errorf("message %q does not contain %q", msg, tt.wantContains)
			}
		})
	}
}

func TestProbeTCP(t *testing.T) {
	tests := []struct {
		name         string
		setupServer  bool // if true, start a TCP listener
		addr         string
		wantCode     int
		wantContains string
	}{
		{
			name:         "successful connection",
			setupServer:  true,
			wantCode:     exitOK,
			wantContains: "healthy",
		},
		{
			name:         "connection refused",
			addr:         "127.0.0.1:1", // port 1 is unlikely to be listening
			wantCode:     exitNonOK,
			wantContains: "failed",
		},
		{
			name:         "missing address",
			addr:         "",
			wantCode:     exitUnknown,
			wantContains: "missing --tcp-addr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := tt.addr
			if tt.setupServer {
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatalf("failed to start TCP listener: %v", err)
				}
				defer ln.Close()
				addr = ln.Addr().String()
			}

			code, msg := probeTCP(addr, 2*time.Second)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (msg: %s)", code, tt.wantCode, msg)
			}
			if !strings.Contains(msg, tt.wantContains) {
				t.Errorf("message %q does not contain %q", msg, tt.wantContains)
			}
		})
	}
}

func TestRun(t *testing.T) {
	// Start an HTTP server for the http probe test.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Start a TCP listener for the tcp probe test.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start TCP listener: %v", err)
	}
	defer ln.Close()

	tests := []struct {
		name         string
		probeType    string
		httpURL      string
		tcpAddr      string
		wantCode     int
		wantContains string
	}{
		{
			name:         "http probe via run",
			probeType:    "http",
			httpURL:      ts.URL,
			wantCode:     exitOK,
			wantContains: "healthy",
		},
		{
			name:         "tcp probe via run",
			probeType:    "tcp",
			tcpAddr:      ln.Addr().String(),
			wantCode:     exitOK,
			wantContains: "healthy",
		},
		{
			name:         "invalid probe type",
			probeType:    "grpc",
			wantCode:     exitUnknown,
			wantContains: "unknown or missing --probe-type",
		},
		{
			name:         "empty probe type",
			probeType:    "",
			wantCode:     exitUnknown,
			wantContains: "unknown or missing --probe-type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg := run(tt.probeType, tt.httpURL, tt.tcpAddr, 2*time.Second, false)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (msg: %s)", code, tt.wantCode, msg)
			}
			if !strings.Contains(msg, tt.wantContains) {
				t.Errorf("message %q does not contain %q", msg, tt.wantContains)
			}
		})
	}
}

func TestHTTPProbeRedirect(t *testing.T) {
	tests := []struct {
		name                   string
		allowNonLocalRedirects bool
		handler                http.HandlerFunc
		wantCode               int
		wantContains           string
	}{
		{
			name:                   "redirect to same host (local) is followed",
			allowNonLocalRedirects: false,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/target" {
					w.WriteHeader(http.StatusOK)
					return
				}
				// Redirect to the same host (local redirect).
				http.Redirect(w, r, "/target", http.StatusFound)
			},
			wantCode:     exitOK,
			wantContains: "healthy: status 200",
		},
		{
			name:                   "redirect to non-local host is blocked by default",
			allowNonLocalRedirects: false,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				// Redirect to a different host — should be blocked.
				w.Header().Set("Location", "http://198.51.100.1/other")
				w.WriteHeader(http.StatusMovedPermanently)
			},
			// The 301 response is used as-is; 301 is in [200,400) → healthy.
			wantCode:     exitOK,
			wantContains: "healthy: status 301",
		},
		{
			name:                   "redirect to non-local host allowed with flag",
			allowNonLocalRedirects: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/target" {
					w.WriteHeader(http.StatusOK)
					return
				}
				// Redirect to the same server but via an absolute URL.
				// With allowNonLocalRedirects=true, the client follows it.
				http.Redirect(w, r, fmt.Sprintf("http://%s/target", r.Host), http.StatusFound)
			},
			wantCode:     exitOK,
			wantContains: "healthy: status 200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(tt.handler)
			defer ts.Close()

			code, msg := probeHTTP(ts.URL, 2*time.Second, tt.allowNonLocalRedirects)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (msg: %s)", code, tt.wantCode, msg)
			}
			if !strings.Contains(msg, tt.wantContains) {
				t.Errorf("message %q does not contain %q", msg, tt.wantContains)
			}
		})
	}
}
