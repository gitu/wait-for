package wait

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseHostPorts(t *testing.T) {
	testCases := []struct {
		name   string
		input  []string
		output []hostPort
		hasErr bool
	}{
		{
			name:   "ValidPorts",
			input:  []string{"127.0.0.1:8080", "192.168.1.10:443"},
			output: []hostPort{{"127.0.0.1", 8080}, {"192.168.1.10", 443}},
			hasErr: false,
		},
		{
			name:   "InvalidPort",
			input:  []string{"127.0.0.1:abcd"},
			hasErr: true,
		},
		{
			name:   "EmptyInput",
			input:  []string{},
			output: []hostPort{},
			hasErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := parseHostPorts(tc.input)
			if (err != nil) != tc.hasErr {
				t.Errorf("parseHostPorts(%v) error = %v, wantErr %v", tc.input, err, tc.hasErr)
				return
			}
			if !tc.hasErr && !compare(res, tc.output) {
				t.Errorf("parseHostPorts(%v) = %v, want %v", tc.input, res, tc.output)
			}
		})
	}
}

func compare(a, b []hostPort) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v.host != b[i].host || v.port != b[i].port {
			return false
		}
	}
	return true
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		status      int
		handleFunc  func(w http.ResponseWriter, r *http.Request)
		shouldPass  bool
		expectedErr error
	}{
		{
			name:   "validURLAndStatus",
			url:    "/",
			status: http.StatusOK,
			handleFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			shouldPass:  true,
			expectedErr: nil,
		},
		{
			name:   "invalidStatus",
			url:    "/",
			status: http.StatusNotFound,
			handleFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			shouldPass:  false,
			expectedErr: errors.New("expected status code 404, got 200"),
		},
		{
			name:        "invalidURL",
			url:         "/invalid",
			status:      http.StatusOK,
			handleFunc:  http.NotFound,
			shouldPass:  false,
			expectedErr: errors.New("expected status code 200, got 404"),
		},
		{
			name:   "timeout",
			url:    "/",
			status: http.StatusOK,
			handleFunc: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(100 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			},
			shouldPass:  false,
			expectedErr: errors.New("context deadline exceeded"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(tc.handleFunc))
			defer ts.Close()

			u := &urlStatus{
				url:    ts.URL + tc.url,
				status: tc.status,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			res, err := u.Check(ctx)
			if res != tc.shouldPass {
				t.Errorf("expected %v, got %v", tc.shouldPass, res)
			}

			if err != nil && !strings.Contains(err.Error(), tc.expectedErr.Error()) {
				t.Errorf("expected %v, got %v", tc.expectedErr, err)
			}
		})
	}
}

func TestParseUrls(t *testing.T) {
	type TestCase struct {
		name        string
		input       []string
		expected    map[string]int
		expectedErr error
	}

	testCases := []TestCase{
		{
			name:  "Correct URL list",
			input: []string{"http://google.com", "http://facebook.com", "301:http://yahoo.com"},
			expected: map[string]int{
				"http://google.com":   200,
				"http://facebook.com": 200,
				"http://yahoo.com":    301,
			},
			expectedErr: nil,
		},
		{
			name:        "URL list with invalid url",
			input:       []string{"http://google.com", "nonexistent", "http://facebook.com"},
			expected:    nil,
			expectedErr: fmt.Errorf("invalid url: nonexistent (parse \"nonexistent\": invalid URI for request)"),
		},
		{
			name:        "URL list with invalid status code",
			input:       []string{"http://google.com", "10a:http://facebook.com"},
			expected:    nil,
			expectedErr: fmt.Errorf("invalid url: 10a:http://facebook.com (parse \"10a:http://facebook.com\": invalid URI for request)"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseUrls(tc.input)
			if err != nil && tc.expectedErr != nil {
				if err.Error() != tc.expectedErr.Error() {
					t.Errorf("got error '%v', want '%v'", err, tc.expectedErr)
				}
			} else if err != tc.expectedErr {
				t.Errorf("got error '%v', want '%v'", err, tc.expectedErr)
			}

			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("got '%v', want '%v'", result, tc.expected)
			}
		})
	}
}

type mockChecker struct {
	healthy bool
	err     error
}

func (m *mockChecker) Name() string                          { return "MockChecker" }
func (m *mockChecker) Check(_ context.Context) (bool, error) { return m.healthy, m.err }

func TestWrappedChecker_Start(t *testing.T) {
	errMock := errors.New("mocked error")
	tests := []struct {
		name           string
		checkerHealthy bool
		checkerError   error
		checkInterval  time.Duration
		expectedStatus *StatusInfo
	}{
		{
			name:           "HealthyCheckerNoError",
			checkerHealthy: true,
			checkerError:   nil,
			checkInterval:  time.Millisecond,
			expectedStatus: &StatusInfo{
				Target:  "MockChecker",
				Healthy: true,
				Error:   nil,
			},
		},
		{
			name:           "UnhealthyCheckerWithError",
			checkerHealthy: false,
			checkerError:   &url.Error{Op: "mockedOp", URL: "mockedUrl", Err: errMock},
			checkInterval:  time.Millisecond,
			expectedStatus: &StatusInfo{
				Target:  "MockChecker",
				Healthy: false,
				Error:   &url.Error{Op: "mockedOp", URL: "mockedUrl", Err: errMock},
			},
		},
		{
			name:           "UnhealthyCheckerNoError",
			checkerHealthy: false,
			checkerError:   nil,
			checkInterval:  time.Millisecond,
			expectedStatus: &StatusInfo{
				Target:  "MockChecker",
				Healthy: false,
				Error:   context.DeadlineExceeded,
			},
		},
		{
			name:           "CheckerWithErrorButHealthyShouldReturnError",
			checkerHealthy: true,
			checkerError:   &url.Error{Op: "mockedOp", URL: "mockedUrl", Err: errMock},
			checkInterval:  time.Millisecond,
			expectedStatus: &StatusInfo{
				Target:  "MockChecker",
				Healthy: false,
				Error:   &url.Error{Op: "mockedOp", URL: "mockedUrl", Err: errMock},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()

			group := &sync.WaitGroup{}

			wrappedChecker := &WrappedChecker{
				Checker:       &mockChecker{healthy: tt.checkerHealthy, err: tt.checkerError},
				CheckInterval: tt.checkInterval,
				LastStatus:    nil,
			}
			group.Add(1)
			wrappedChecker.Start(ctx, group)
			group.Wait()

			if !reflect.DeepEqual(wrappedChecker.LastStatus, tt.expectedStatus) {
				t.Errorf("Expected status %v, but got %v", tt.expectedStatus, wrappedChecker.LastStatus)
			}
		})
	}
}

func setupHTTPServer(listenPort string) string {
	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, "Hello, HTTP Server!")
		})
		if err := http.ListenAndServe(listenPort, nil); err != nil {
			log.Fatal(err)
		}
	}()
	return "localhost"
}

func TestHostPort_Check(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		port        int
		deadline    time.Duration
		want        bool
		expectedErr error
	}{
		{
			name:        "ValidHostAndPort",
			host:        setupHTTPServer(":53231"),
			port:        53231,
			deadline:    5 * time.Second,
			want:        true,
			expectedErr: nil,
		},
		{
			name:        "NoDeadlineSet",
			host:        "localhost",
			port:        53231,
			deadline:    0,
			want:        false,
			expectedErr: fmt.Errorf("no deadline set"),
		},
		{
			name:        "NoDeadlineSet",
			host:        "127.0.0.1",
			port:        53232,
			deadline:    50 * time.Millisecond,
			want:        false,
			expectedErr: fmt.Errorf("dial tcp 127.0.0.1:53232"),
		},
		{
			name:        "UnresolvedHost",
			host:        "unknownhostwithnoassociatedip",
			port:        8000,
			deadline:    5 * time.Second,
			want:        false,
			expectedErr: fmt.Errorf("lookup unknownhostwithnoassociatedip"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &hostPort{
				host: tt.host,
				port: tt.port,
			}
			ctx := context.Background()
			if tt.deadline != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), tt.deadline)
				defer cancel()
			}
			got, err := h.Check(ctx)
			if got != tt.want {
				t.Errorf("hostPort.Check() = %v, want %v", got, tt.want)
			}
			if err != nil && !strings.Contains(err.Error(), tt.expectedErr.Error()) {
				t.Errorf("hostPort.Check() error = %v, want %v", err, tt.expectedErr)
			}
		})
	}
}
