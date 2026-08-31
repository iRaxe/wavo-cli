package sdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type diagnosticErrorReader struct {
	err error
}

func (r diagnosticErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type finiteByteReader struct {
	remaining int64
	read      int64
	value     byte
}

func (r *finiteByteReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	for i := range buffer {
		buffer[i] = r.value
	}
	r.remaining -= int64(len(buffer))
	r.read += int64(len(buffer))
	return len(buffer), nil
}

const rawSocketTestTimeout = 5 * time.Second

func TestDefaultClientRefusesCrossOriginRedirectBeforeSendingToken(t *testing.T) {
	const token = "API:error"
	for _, request := range []struct {
		name string
		do   func(*Client) error
	}{
		{name: "raw", do: func(client *Client) error {
			var response map[string]any
			return client.GetRaw("/api/v1/profile", nil, &response)
		}},
		{name: "account scoped", do: func(client *Client) error {
			var response map[string]any
			return client.Get("/items", nil, &response)
		}},
	} {
		t.Run(request.name, func(t *testing.T) {
			var targetRequests atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetRequests.Add(1)
				if got := r.Header.Get("api_access_token"); got != "" {
					t.Errorf("redirect target received token %q", got)
				}
				_, _ = io.WriteString(w, `{}`)
			}))
			defer target.Close()
			location := target.URL + "/capture?raw=" + token + "&mixed=AP%49%3aerror&lower=%41%50%49%3a%65%72%72%6f%72"
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", location)
				w.WriteHeader(http.StatusFound)
			}))
			defer source.Close()

			err := request.do(NewClient(source.URL, token, 7))
			if err == nil || !strings.Contains(err.Error(), "redirect refused") {
				t.Fatalf("request error = %v, want generic redirect rejection", err)
			}
			for _, secret := range []string{token, source.URL, target.URL, location, "AP%49%3aerror", "%41%50%49%3a%65%72%72%6f%72"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("redirect error exposed %q: %q", secret, err)
				}
			}
			if got := targetRequests.Load(); got != 0 {
				t.Fatalf("redirect target received %d requests, want 0", got)
			}
		})
	}
}

func TestAuthenticatedDiagnosticsNeverContainActiveCredential(t *testing.T) {
	tokens := []string{"API", "GET", "error", ":", "[REDACTED]", "ordinary-long-active-token"}
	for _, token := range tokens {
		t.Run(token, func(t *testing.T) {
			scenarios := []struct {
				name string
				run  func() (string, error)
			}{
				{
					name: "verbose success",
					run: func() (string, error) {
						httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
							return &http.Response{
								StatusCode: http.StatusOK,
								Status:     "200 OK",
								Header:     make(http.Header),
								Body:       io.NopCloser(strings.NewReader(`{"name":"safe ` + token + ` value"}`)),
								Request:    request,
							}, nil
						})}
						client := NewClient("https://wavo.example", token, 1, WithHTTPClient(httpClient), WithVerbose(true))
						var decoded map[string]any
						var requestErr error
						stderr := captureStderr(t, func() { requestErr = client.Get("/profile", nil, &decoded) })
						return stderr, requestErr
					},
				},
				{
					name: "transport failure",
					run: func() (string, error) {
						httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
							return nil, errors.New("transport diagnostic " + token)
						})}
						client := NewClient("https://wavo.example", token, 1, WithHTTPClient(httpClient), WithVerbose(true))
						var decoded map[string]any
						var requestErr error
						stderr := captureStderr(t, func() { requestErr = client.Get("/profile", nil, &decoded) })
						return stderr, requestErr
					},
				},
				{
					name: "redirect refusal",
					run: func() (string, error) {
						target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
							_, _ = io.WriteString(w, `{}`)
						}))
						defer target.Close()
						source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
							w.Header().Set("Location", target.URL)
							w.WriteHeader(http.StatusFound)
						}))
						defer source.Close()
						client := NewClient(source.URL, token, 1, WithVerbose(true))
						var decoded map[string]any
						var requestErr error
						stderr := captureStderr(t, func() { requestErr = client.GetRaw("/profile", nil, &decoded) })
						return stderr, requestErr
					},
				},
				{
					name: "body read failure",
					run: func() (string, error) {
						httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
							return &http.Response{
								StatusCode: http.StatusOK,
								Status:     "200 OK",
								Header:     make(http.Header),
								Body:       io.NopCloser(diagnosticErrorReader{err: errors.New("body diagnostic " + token)}),
								Request:    request,
							}, nil
						})}
						client := NewClient("https://wavo.example", token, 1, WithHTTPClient(httpClient), WithVerbose(true))
						var requestErr error
						stderr := captureStderr(t, func() {
							_, requestErr = client.RequestRaw(http.MethodGet, "/profile", nil, false, nil)
						})
						return stderr, requestErr
					},
				},
				{
					name: "decode failure",
					run: func() (string, error) {
						httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
							return &http.Response{
								StatusCode: http.StatusOK,
								Status:     "200 OK",
								Header:     make(http.Header),
								Body:       io.NopCloser(strings.NewReader(`{"broken":` + token)),
								Request:    request,
							}, nil
						})}
						client := NewClient("https://wavo.example", token, 1, WithHTTPClient(httpClient), WithVerbose(true))
						var decoded map[string]any
						var requestErr error
						stderr := captureStderr(t, func() { requestErr = client.Get("/profile", nil, &decoded) })
						return stderr, requestErr
					},
				},
				{
					name: "API error",
					run: func() (string, error) {
						httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
							return &http.Response{
								StatusCode: http.StatusUnauthorized,
								Status:     "401 Unauthorized",
								Header:     make(http.Header),
								Body:       io.NopCloser(strings.NewReader(`{"message":"safe ` + token + ` detail"}`)),
								Request:    request,
							}, nil
						})}
						client := NewClient("https://wavo.example", token, 1, WithHTTPClient(httpClient), WithVerbose(true))
						var decoded map[string]any
						var requestErr error
						stderr := captureStderr(t, func() { requestErr = client.Get("/profile", nil, &decoded) })
						return stderr, requestErr
					},
				},
			}

			for _, scenario := range scenarios {
				t.Run(scenario.name, func(t *testing.T) {
					stderr, requestErr := scenario.run()
					combined := stderr
					if requestErr != nil {
						combined += requestErr.Error()
					}
					if strings.Contains(combined, token) {
						t.Fatalf("authenticated diagnostic exposed active credential %q: error=%v stderr=%q", token, requestErr, stderr)
					}
					if unwrapped := errors.Unwrap(requestErr); unwrapped != nil && strings.Contains(unwrapped.Error(), token) {
						t.Fatalf("authenticated diagnostic exposed credential through unwrapping: %q", unwrapped)
					}
				})
			}
		})
	}
}

func TestDefaultClientStopsAfterTenSameOriginRequests(t *testing.T) {
	const token = "redirect-limit-secret"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount := requests.Add(1)
		if got := r.Header.Get("api_access_token"); got != token {
			t.Errorf("allowed hop token = %q, want %q", got, token)
		}
		if requestCount > 12 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
			return
		}
		hop, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/hop/"))
		if err != nil {
			t.Errorf("invalid hop path %q: %v", r.URL.Path, err)
			http.Error(w, "bad hop", http.StatusBadRequest)
			return
		}
		w.Header().Set("Location", "/hop/"+strconv.Itoa(hop+1)+"?secret="+token)
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	var response map[string]any
	err := NewClient(server.URL, token, 7).GetRaw("/hop/0", nil, &response)
	if err == nil || !strings.Contains(err.Error(), "redirect refused") {
		t.Fatalf("request error = %v, want generic redirect-limit rejection", err)
	}
	if got := requests.Load(); got != 10 {
		t.Fatalf("server received %d requests, want exactly 10", got)
	}
	for _, secret := range []string{token, server.URL, "/hop/10", "secret="} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("redirect-limit error exposed %q: %q", secret, err)
		}
	}
}

func TestDefaultClientAllowsSameOriginRedirect(t *testing.T) {
	const token = "redirect-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			if got := r.Header.Get("api_access_token"); got != token {
				t.Errorf("same-origin redirect token = %q, want %q", got, token)
			}
			_, _ = io.WriteString(w, `{}`)
			return
		}
		http.Redirect(w, r, "/final", http.StatusFound)
	}))
	defer server.Close()

	var response map[string]any
	if err := NewClient(server.URL, token, 7).GetRaw("/api/v1/profile", nil, &response); err != nil {
		t.Fatalf("same-origin redirect failed: %v", err)
	}
}

func TestTransportErrorsAfterCredentialBearingRedirectAreGeneric(t *testing.T) {
	const token = "transport+token/API"
	queryEncoded := url.QueryEscape(token)
	pathEncoded := url.PathEscape(token)
	location := "/close/" + token + "?query=" + queryEncoded + "&path=" + pathEncoded
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusFound)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/close/") {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("server does not support hijacking")
			return
		}
		connection, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(server.Close)

	for _, test := range []struct {
		name string
		do   func(*Client) error
	}{
		{
			name: "decoded account request",
			do: func(client *Client) error {
				var decoded map[string]any
				return client.Get("/start", nil, &decoded)
			},
		},
		{
			name: "raw request",
			do: func(client *Client) error {
				_, err := client.RequestRaw(http.MethodGet, "/start", nil, false, nil)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(server.URL, token, 7, WithHTTPClient(server.Client()), WithVerbose(true))
			var requestErr error
			stderr := captureStderr(t, func() {
				requestErr = test.do(client)
			})
			if !errors.Is(requestErr, errAPIRequestFailed) {
				t.Fatalf("request error = %v, want generic API request failure", requestErr)
			}
			for _, leaked := range []string{token, queryEncoded, pathEncoded, location, server.URL + location} {
				if strings.Contains(requestErr.Error(), leaked) || strings.Contains(stderr, leaked) {
					t.Fatalf("transport failure exposed %q: error=%q stderr=%q", leaked, requestErr, stderr)
				}
			}
		})
	}
}

func TestTransportErrorUsesLowercaseWavoDiagnostic(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("untrusted transport detail")
	})}
	client := NewClient("https://wavo.example", "ordinary-active-token", 7, WithHTTPClient(httpClient))
	var decoded map[string]any

	err := client.Get("/profile", nil, &decoded)
	if err == nil || err.Error() != "request to Wavo API failed" || !errors.Is(err, errAPIRequestFailed) {
		t.Fatalf("request error = %v, want lowercase generic Wavo diagnostic", err)
	}
}

func TestMalformedCredentialBearingStatusLineReturnsGenericError(t *testing.T) {
	const token = "malformed-status-token"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	setListenerDeadline(t, listener)
	serveResult := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serveResult <- err
			return
		}
		defer func() { _ = connection.Close() }()
		_ = connection.SetDeadline(time.Now().Add(rawSocketTestTimeout))
		request := make([]byte, 4096)
		if _, err := connection.Read(request); err != nil {
			serveResult <- err
			return
		}
		_, err = io.WriteString(connection, "HTTP/1.1 "+token+"\r\nContent-Length: 0\r\n\r\n")
		serveResult <- err
	}()
	baseURL := "http://" + listener.Addr().String()
	client := NewClient(baseURL, token, 1, WithVerbose(true))
	var decoded map[string]any
	var requestErr error
	stderr := captureStderr(t, func() {
		requestErr = client.GetRaw("/profile", nil, &decoded)
	})
	if err := waitServeResult(t, serveResult); err != nil {
		t.Fatalf("malformed server: %v", err)
	}
	if !errors.Is(requestErr, errAPIRequestFailed) {
		t.Fatalf("request error = %v, want generic API request failure", requestErr)
	}
	if strings.Contains(requestErr.Error(), token) || strings.Contains(stderr, token) {
		t.Fatalf("malformed status error exposed token: error=%q stderr=%q", requestErr, stderr)
	}
}

func TestMalformedCredentialBearingChunkedBodyReturnsSafeError(t *testing.T) {
	const token = "active trailer+token/API"
	for _, test := range []struct {
		name    string
		verbose bool
		wantErr string
		do      func(*Client) error
	}{
		{
			name:    "decoded verbose",
			verbose: true,
			wantErr: "failed to read Wavo API response",
			do: func(client *Client) error {
				var decoded map[string]any
				return client.GetRaw("/profile", nil, &decoded)
			},
		},
		{
			name:    "decoded non-verbose",
			wantErr: "failed to decode Wavo API response",
			do: func(client *Client) error {
				var decoded map[string]any
				return client.GetRaw("/profile", nil, &decoded)
			},
		},
		{
			name:    "raw verbose",
			verbose: true,
			wantErr: "failed to read Wavo API response",
			do: func(client *Client) error {
				_, err := client.RequestRaw(http.MethodGet, "/profile", nil, false, nil)
				return err
			},
		},
		{
			name:    "raw non-verbose",
			wantErr: "failed to read Wavo API response",
			do: func(client *Client) error {
				_, err := client.RequestRaw(http.MethodGet, "/profile", nil, false, nil)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			baseURL, serveResult := malformedChunkedServer(t, token, "200 OK")
			client := NewClient(baseURL, token, 1, WithVerbose(test.verbose))
			var requestErr error
			stderr := captureStderr(t, func() {
				requestErr = test.do(client)
			})
			if err := waitServeResult(t, serveResult); err != nil {
				t.Fatalf("malformed chunk server: %v", err)
			}
			if requestErr == nil || requestErr.Error() != test.wantErr {
				t.Fatalf("request error = %v, want constant %q", requestErr, test.wantErr)
			}
			for _, leaked := range []string{token, "trailer", baseURL} {
				if strings.Contains(requestErr.Error(), leaked) {
					t.Fatalf("body-read error exposed %q: %q", leaked, requestErr)
				}
			}
			if strings.Contains(stderr, token) {
				t.Fatalf("verbose body-read output exposed token: %q", stderr)
			}
		})
	}
}

func TestAPIErrorWithMalformedCredentialBearingBodyReturnsSafeReadError(t *testing.T) {
	const token = "error trailer+token/API"
	for _, test := range []struct {
		name string
		do   func(*Client) error
	}{
		{
			name: "decoded",
			do: func(client *Client) error {
				var decoded map[string]any
				return client.GetRaw("/profile", nil, &decoded)
			},
		},
		{
			name: "raw",
			do: func(client *Client) error {
				_, err := client.RequestRaw(http.MethodGet, "/profile", nil, false, nil)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			baseURL, serveResult := malformedChunkedServer(t, token, "401 Unauthorized")
			requestErr := test.do(NewClient(baseURL, token, 1))
			if err := waitServeResult(t, serveResult); err != nil {
				t.Fatalf("malformed chunk server: %v", err)
			}
			if requestErr == nil || requestErr.Error() != "failed to read Wavo API response" {
				t.Fatalf("request error = %v, want constant read error", requestErr)
			}
			for _, leaked := range []string{token, "trailer", baseURL, "API error 401"} {
				if strings.Contains(requestErr.Error(), leaked) {
					t.Fatalf("malformed API body exposed %q: %q", leaked, requestErr)
				}
			}
		})
	}
}

func TestMalformedCredentialBearingJSONReturnsSafeDecodeError(t *testing.T) {
	const token = "malformed json+token/API"
	for _, verbose := range []bool{false, true} {
		t.Run(fmt.Sprintf("verbose=%v", verbose), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"name":"`+token+`","broken":`+token)
			}))
			defer server.Close()
			client := NewClient(server.URL, token, 1, WithHTTPClient(server.Client()), WithVerbose(verbose))
			var decoded map[string]any
			var requestErr error
			stderr := captureStderr(t, func() {
				requestErr = client.Get("/profile", nil, &decoded)
			})
			if requestErr == nil || requestErr.Error() != "failed to decode Wavo API response" {
				t.Fatalf("request error = %v, want constant decode error", requestErr)
			}
			if strings.Contains(requestErr.Error(), token) || strings.Contains(stderr, token) {
				t.Fatalf("decode failure exposed token: error=%q stderr=%q", requestErr, stderr)
			}
		})
	}
}

func TestNonVerboseDecodedResponseStreamsLargeTrailingWhitespace(t *testing.T) {
	const trailingWhitespaceBytes = 2 << 20
	trailing := &finiteByteReader{remaining: trailingWhitespaceBytes, value: ' '}
	body := io.MultiReader(strings.NewReader(`{"ok":true}`), trailing)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(body),
			Request:    request,
		}, nil
	})}
	client := NewClient("https://wavo.example", "streaming-secret", 1, WithHTTPClient(httpClient))

	var decoded map[string]bool
	if err := client.Get("/profile", nil, &decoded); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !decoded["ok"] {
		t.Fatalf("decoded response = %#v, want ok=true", decoded)
	}
	if trailing.remaining != 0 {
		t.Fatalf("trailing response bytes remaining = %d, want complete validation", trailing.remaining)
	}
}

func TestNonVerboseDecodedResponseRejectsExtraJSON(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true} {"extra":"active-token"}`)),
			Request:    request,
		}, nil
	})}
	client := NewClient("https://wavo.example", "active-token", 1, WithHTTPClient(httpClient))

	var decoded map[string]bool
	err := client.Get("/profile", nil, &decoded)
	if !errors.Is(err, errAPIResponseDecode) || err.Error() != "failed to decode Wavo API response" {
		t.Fatalf("Get error = %v, want constant decode error", err)
	}
	if strings.Contains(err.Error(), "active-token") || strings.Contains(err.Error(), "extra") {
		t.Fatalf("decode error exposed trailing response: %q", err)
	}
}

func TestVerboseDecodedResponseRejectsOversizedBody(t *testing.T) {
	const verboseLimit = 8 << 20
	body := &finiteByteReader{remaining: verboseLimit + 2, value: 'x'}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(body),
			Request:    request,
		}, nil
	})}
	client := NewClient("https://wavo.example", "oversized-secret", 1, WithHTTPClient(httpClient), WithVerbose(true))

	var decoded map[string]any
	var requestErr error
	stderr := captureStderr(t, func() {
		requestErr = client.Get("/profile", nil, &decoded)
	})
	if requestErr == nil || requestErr.Error() != "response from Wavo API too large" {
		t.Fatalf("Get error = %v, want constant oversized-response error", requestErr)
	}
	if body.read != verboseLimit+1 {
		t.Fatalf("response bytes read = %d, want bounded %d", body.read, verboseLimit+1)
	}
	if strings.Contains(requestErr.Error(), "oversized-secret") || strings.Contains(stderr, "oversized-secret") {
		t.Fatalf("oversized response exposed credential: error=%q stderr=%q", requestErr, stderr)
	}
}

func TestAPIErrorBodyIsBoundedAndSafelyOmitted(t *testing.T) {
	const errorBodyLimit = 1 << 20
	for _, request := range []struct {
		name string
		do   func(*Client) error
	}{
		{name: "decoded", do: func(client *Client) error {
			var decoded map[string]any
			return client.Get("/boom", nil, &decoded)
		}},
		{name: "raw", do: func(client *Client) error {
			_, err := client.RequestRaw(http.MethodGet, "/boom", nil, false, nil)
			return err
		}},
	} {
		t.Run(request.name, func(t *testing.T) {
			body := &finiteByteReader{remaining: errorBodyLimit + 2, value: 'x'}
			httpClient := &http.Client{Transport: roundTripFunc(func(httpRequest *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     make(http.Header),
					Body:       io.NopCloser(body),
					Request:    httpRequest,
				}, nil
			})}
			client := NewClient("https://wavo.example", "oversized-error-secret", 1, WithHTTPClient(httpClient))

			err := request.do(client)
			if err == nil || err.Error() != "API error 401: response body omitted because it exceeds the diagnostic limit" {
				t.Fatalf("request error = %v, want bounded generic API error", err)
			}
			if body.read != errorBodyLimit+1 {
				t.Fatalf("error body bytes read = %d, want bounded %d", body.read, errorBodyLimit+1)
			}
			if strings.Contains(err.Error(), "oversized-error-secret") || strings.Contains(err.Error(), "xxxx") {
				t.Fatalf("oversized API error exposed body or credential: %q", err)
			}
		})
	}
}

func TestRawSuccessfulResponseRemainsUnboundedByDiagnosticLimit(t *testing.T) {
	const bodySize = (1 << 20) + 1
	body := &finiteByteReader{remaining: bodySize, value: 'r'}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(body),
			Request:    request,
		}, nil
	})}
	client := NewClient("https://wavo.example", "raw-success-secret", 1, WithHTTPClient(httpClient))

	response, err := client.RequestRaw(http.MethodGet, "/profile", nil, false, nil)
	if err != nil {
		t.Fatalf("RequestRaw: %v", err)
	}
	if len(response.Body) != bodySize {
		t.Fatalf("raw body size = %d, want %d", len(response.Body), bodySize)
	}
}

func malformedChunkedServer(t *testing.T, token, status string) (string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	setListenerDeadline(t, listener)
	serveResult := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serveResult <- err
			return
		}
		defer func() { _ = connection.Close() }()
		_ = connection.SetDeadline(time.Now().Add(rawSocketTestTimeout))
		request := make([]byte, 4096)
		if _, err := connection.Read(request); err != nil {
			serveResult <- err
			return
		}
		response := "HTTP/1.1 " + status + "\r\n" +
			"Transfer-Encoding: chunked\r\n" +
			"Trailer: X-Wavo\r\n\r\n" +
			"2\r\n{}\r\n" +
			"0\r\nmalformed trailer " + token + "\r\n\r\n"
		_, err = io.WriteString(connection, response)
		serveResult <- err
	}()
	return "http://" + listener.Addr().String(), serveResult
}

func setListenerDeadline(t *testing.T, listener net.Listener) {
	t.Helper()
	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		t.Fatalf("listener type = %T, want *net.TCPListener", listener)
	}
	if err := tcpListener.SetDeadline(time.Now().Add(rawSocketTestTimeout)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}
}

func waitServeResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(rawSocketTestTimeout):
		t.Fatal("timed out waiting for raw HTTP test server")
		return nil
	}
}

func TestSameOriginUsesSchemeHostnameAndEffectivePort(t *testing.T) {
	parse := func(raw string) *url.URL {
		t.Helper()
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", raw, err)
		}
		return u
	}
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{name: "HTTPS default port", from: "https://wavo.example/start", to: "https://WAVO.example:443/end", want: true},
		{name: "HTTP default port", from: "http://wavo.example/start", to: "http://wavo.example:80/end", want: true},
		{name: "HTTPS downgrade", from: "https://wavo.example/start", to: "http://wavo.example/end", want: false},
		{name: "different port", from: "https://wavo.example/start", to: "https://wavo.example:444/end", want: false},
		{name: "different hostname", from: "https://wavo.example/start", to: "https://evil.example/end", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameOrigin(parse(tt.from), parse(tt.to)); got != tt.want {
				t.Fatalf("sameOrigin(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestVerboseAssignDoesNotLeakSensitiveUserTokens(t *testing.T) {
	const (
		accessSecret = "assign-access-secret"
		pubsubSecret = "assign-pubsub-secret"
	)

	client := newContractClient(t, func(t *testing.T, r *http.Request, _ *openapi3filter.RequestValidationInput) contractResponse {
		assertJSONBody(t, r, map[string]any{
			"assignee_id": float64(7),
		})

		return contractResponse{body: sensitiveUserJSON(7, "Ada Lovelace", "ada@example.com", accessSecret, pubsubSecret)}
	})
	client.Verbose = true

	agentID := 7
	stderr := captureStderr(t, func() {
		got, err := client.Conversations().Assign(42, &agentID, 0)
		if err != nil {
			t.Fatalf("Assign returned error: %v", err)
		}
		if got.AccessToken != accessSecret || got.PubsubToken != pubsubSecret {
			t.Fatalf("unexpected token decode: %#v", got)
		}
	})

	assertDoesNotContainSensitiveValues(t, stderr, accessSecret, pubsubSecret)
}

func TestVerboseResponseLoggingDoesNotLeakNestedSensitiveTokens(t *testing.T) {
	const (
		accessSecret = "nested-access-secret"
		pubsubSecret = "nested-pubsub-secret"
		hmacSecret   = "nested-hmac-secret"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"payload": {
				"user": {
					"id": 7,
					"access_token": "`+accessSecret+`",
					"pubsub_token": "`+pubsubSecret+`",
					"hmac_identifier": "`+hmacSecret+`"
				}
			}
		}`)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "api-key", 1, WithHTTPClient(server.Client()), WithVerbose(true))

	var decoded map[string]any
	stderr := captureStderr(t, func() {
		if err := client.Get("/users/7", nil, &decoded); err != nil {
			t.Fatalf("Get returned error: %v", err)
		}
	})

	assertDoesNotContainSensitiveValues(t, stderr, accessSecret, pubsubSecret, hmacSecret)
}

func TestVerboseResponseLoggingRedactsSecretLikeFields(t *testing.T) {
	const (
		tokenSecret = "generic-token-secret"
		apiSecret   = "generic-api-secret"
		signingKey  = "generic-signing-key"
		hmacValue   = "generic-hmac-value"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"payload": {
				"session_token": "`+tokenSecret+`",
				"webhook_secret": "`+apiSecret+`",
				"signing_key": "`+signingKey+`",
				"hmac_digest": "`+hmacValue+`"
			}
		}`)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "api-key", 1, WithHTTPClient(server.Client()), WithVerbose(true))

	var decoded map[string]any
	stderr := captureStderr(t, func() {
		if err := client.Get("/secrets", nil, &decoded); err != nil {
			t.Fatalf("Get returned error: %v", err)
		}
	})

	assertDoesNotContainSensitiveValues(t, stderr, tokenSecret, apiSecret, signingKey, hmacValue)
}

func TestVerboseResponseLoggingRedactsActiveCredentialFromEveryStringLeaf(t *testing.T) {
	const token = `a b+c/"quoted\token`
	queryEncoded := url.QueryEscape(token)
	pathEncoded := url.PathEscape(token)
	lowerEncoded := strings.ToLower(queryEncoded)
	body, err := json.Marshal(map[string]any{
		"name":  "safe name before " + token + " after",
		"email": queryEncoded,
		"items": []string{pathEncoded, lowerEncoded, "ordinary safe value"},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, token, 1, WithHTTPClient(server.Client()), WithVerbose(true))

	var decoded map[string]any
	stderr := captureStderr(t, func() {
		if err := client.Get("/profile", nil, &decoded); err != nil {
			t.Fatalf("Get: %v", err)
		}
	})
	escapedTokenJSON, err := json.Marshal(token)
	if err != nil {
		t.Fatalf("Marshal(token): %v", err)
	}
	escapedToken := string(escapedTokenJSON[1 : len(escapedTokenJSON)-1])
	assertDoesNotContainSensitiveValues(t, stderr, token, queryEncoded, pathEncoded, lowerEncoded, escapedToken)
	if !strings.Contains(stderr, "ordinary safe value") {
		t.Fatalf("verbose output removed safe ordinary field:\n%s", stderr)
	}
	if decoded["name"] != "safe name before "+token+" after" {
		t.Fatalf("machine response was mutated: %#v", decoded)
	}
}

func TestVerboseResponseLoggingRedactsActiveCredentialFromObjectKeys(t *testing.T) {
	const token = "active token+API/error"
	queryEncoded := url.QueryEscape(token)
	pathEncoded := url.PathEscape(token)
	body, err := json.Marshal(map[string]any{
		"ordinary": "safe ordinary value",
		token: map[string]any{
			queryEncoded: map[string]any{
				pathEncoded: "safe nested value",
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, token, 1, WithHTTPClient(server.Client()), WithVerbose(true))

	var decoded map[string]any
	stderr := captureStderr(t, func() {
		if err := client.Get("/profile", nil, &decoded); err != nil {
			t.Fatalf("Get: %v", err)
		}
	})
	assertDoesNotContainSensitiveValues(t, stderr, token, queryEncoded, pathEncoded)
	for _, safe := range []string{"safe ordinary value", "safe nested value"} {
		if !strings.Contains(stderr, safe) {
			t.Fatalf("verbose output removed %q:\n%s", safe, stderr)
		}
	}
	if _, ok := decoded[token]; !ok {
		t.Fatalf("machine response keys were mutated: %#v", decoded)
	}
}

func TestAPIErrorBodyRedactsActiveCredentialFromObjectKeys(t *testing.T) {
	const token = "active token+API/error"
	queryEncoded := url.QueryEscape(token)
	pathEncoded := url.PathEscape(token)
	body, err := json.Marshal(map[string]any{
		token: map[string]any{
			queryEncoded: map[string]any{pathEncoded: "safe error detail"},
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	var decoded map[string]any
	err = NewClient(server.URL, token, 1, WithHTTPClient(server.Client())).Get("/boom", nil, &decoded)
	if err == nil || !strings.Contains(err.Error(), "API error 401") {
		t.Fatalf("error = %v, want HTTP status", err)
	}
	assertDoesNotContainSensitiveValues(t, err.Error(), token, queryEncoded, pathEncoded)
	if !strings.Contains(err.Error(), "safe error detail") {
		t.Fatalf("error removed safe nested detail: %q", err)
	}
}

func TestRedactSensitiveJSONKeyCollisionUsesSortedLast(t *testing.T) {
	const token = "active token+API/error"
	queryEncoded := url.QueryEscape(token)
	body, err := json.Marshal(map[string]string{
		token:        "raw value",
		queryEncoded: "query value",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	redacted := redactSensitiveJSON(body, token)
	assertDoesNotContainSensitiveValues(t, redacted, token, queryEncoded)
	var decoded map[string]string
	if err := json.Unmarshal([]byte(redacted), &decoded); err != nil {
		t.Fatalf("Unmarshal(redacted): %v; body=%s", err, redacted)
	}
	if got := decoded["[REDACTED]"]; got != "query value" {
		t.Fatalf("redacted collision value = %q, want deterministic sorted-last query value; body=%s", got, redacted)
	}
}

func TestVerboseResponseStatusRedactsActiveCredential(t *testing.T) {
	const token = "status-reflected-token"
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 " + token,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Request:    request,
		}, nil
	})}
	client := NewClient("https://wavo.example", token, 1, WithHTTPClient(httpClient), WithVerbose(true))

	var decoded map[string]any
	stderr := captureStderr(t, func() {
		if err := client.Get("/profile", nil, &decoded); err != nil {
			t.Fatalf("Get: %v", err)
		}
	})
	assertDoesNotContainSensitiveValues(t, stderr, token)
}

func TestVerboseNonJSONResponseLoggingRedactsActiveCredential(t *testing.T) {
	const token = "non-json-active-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ordinary before "+token+" after")
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, token, 1, WithHTTPClient(server.Client()), WithVerbose(true))

	var decoded map[string]any
	stderr := captureStderr(t, func() {
		if err := client.Get("/plain", nil, &decoded); err == nil {
			t.Fatal("expected JSON decode error")
		}
	})
	assertDoesNotContainSensitiveValues(t, stderr, token)
	if !strings.Contains(stderr, "ordinary before") || !strings.Contains(stderr, "after") {
		t.Fatalf("verbose output removed safe non-JSON text:\n%s", stderr)
	}
}

func TestAPIErrorBodyRedactsActiveCredential(t *testing.T) {
	const token = "api error+token/value"
	queryEncoded := url.QueryEscape(token)
	pathEncoded := url.PathEscape(token)
	for _, test := range []struct {
		name string
		body string
		do   func(*Client) error
	}{
		{
			name: "JSON decoded request",
			body: `{"message":"safe prefix ` + token + ` suffix","ordinary":"keep me","encoded":["` + queryEncoded + `","` + pathEncoded + `"]}`,
			do: func(client *Client) error {
				var decoded map[string]any
				return client.Get("/boom", nil, &decoded)
			},
		},
		{
			name: "non-JSON raw request",
			body: "safe prefix " + token + " suffix",
			do: func(client *Client) error {
				_, err := client.RequestRaw(http.MethodGet, "/boom", nil, false, nil)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			err := test.do(NewClient(server.URL, token, 1, WithHTTPClient(server.Client())))
			if err == nil || !strings.Contains(err.Error(), "API error 401") {
				t.Fatalf("error = %v, want HTTP status", err)
			}
			assertDoesNotContainSensitiveValues(t, err.Error(), token, queryEncoded, pathEncoded)
			if !strings.Contains(err.Error(), "safe prefix") {
				t.Fatalf("error removed safe body context: %q", err)
			}
		})
	}
}

func TestVerboseNonJSONResponseLoggingStripsTerminalControls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "hello\x1b]52;c;Zm9v\aworld\x1b[31m")
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "api-key", 1, WithHTTPClient(server.Client()), WithVerbose(true))

	var decoded map[string]any
	stderr := captureStderr(t, func() {
		if err := client.Get("/plain", nil, &decoded); err == nil {
			t.Fatal("expected JSON decode error")
		}
	})

	assertDoesNotContainSensitiveValues(t, stderr, "\x1b", "\a", "]52", "[31m")
	if !strings.Contains(stderr, "helloworld") {
		t.Fatalf("verbose output stripped printable content:\n%s", stderr)
	}
}

func TestAPIErrorBodyStripsTerminalControls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad\x1b]52;c;Zm9v\aerror", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "api-key", 1, WithHTTPClient(server.Client()))

	var decoded map[string]any
	err := client.Get("/boom", nil, &decoded)
	if err == nil {
		t.Fatal("expected API error")
	}
	got := err.Error()
	for _, disallowed := range []string{"\x1b", "\a", "]52"} {
		if strings.Contains(got, disallowed) {
			t.Fatalf("API error contained terminal control %q: %q", disallowed, got)
		}
	}
	if !strings.Contains(got, "baderror") {
		t.Fatalf("API error stripped printable content: %q", got)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}

	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = original
		_ = reader.Close()
		_ = writer.Close()
	})

	output := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		output <- buf.String()
	}()

	fn()

	_ = writer.Close()
	os.Stderr = original
	return <-output
}

func assertDoesNotContainSensitiveValues(t *testing.T, output string, values ...string) {
	t.Helper()

	for _, value := range values {
		if strings.Contains(output, value) {
			t.Fatalf("verbose output leaked sensitive value %q:\n%s", value, output)
		}
	}
}

func sensitiveUserJSON(id int, name, email, accessToken, pubsubToken string) string {
	body := userJSON(id, name, email)
	body = strings.Replace(body, `"access_token": "token"`, `"access_token": "`+accessToken+`"`, 1)
	body = strings.Replace(body, `"pubsub_token": "pubsub"`, `"pubsub_token": "`+pubsubToken+`"`, 1)
	return body
}
