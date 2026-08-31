package sdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"github.com/iRaxe/wavo-cli/internal/output"
)

var (
	errRedirectRefused     = errors.New("redirect refused")
	errAPIRequestFailed    = errors.New("request to " + brand.Product + " API failed")
	errAPIResponseRead     = errors.New("failed to read " + brand.Product + " API response")
	errAPIResponseDecode   = errors.New("failed to decode " + brand.Product + " API response")
	errAPIResponseTooLarge = errors.New("response from " + brand.Product + " API too large")
)

const (
	// Verbose decoded responses must be buffered so they can be logged and then
	// decoded. Cap that diagnostic buffer to avoid unbounded memory use.
	maxVerboseDecodedResponseBytes int64 = 8 << 20
	// API error bodies are diagnostics rather than machine responses, so retain
	// at most 1 MiB before omitting an oversized body from the returned error.
	maxAPIErrorDiagnosticBytes int64 = 1 << 20
)

type Client struct {
	BaseURL    string
	APIKey     string
	AccountID  int
	Verbose    bool
	httpClient *http.Client
}

type RawResponse struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
}

type credentialSafeError struct {
	kind    error
	message string
}

func (e *credentialSafeError) Error() string { return e.message }

func (e *credentialSafeError) Is(target error) bool { return target == e.kind }

type ClientOption func(*Client)

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

func WithVerbose(verbose bool) ClientOption {
	return func(c *Client) {
		c.Verbose = verbose
	}
}

func NewClient(baseURL, apiKey string, accountID int, opts ...ClientOption) *Client {
	c := &Client{
		BaseURL:   strings.TrimSuffix(baseURL, "/"),
		APIKey:    apiKey,
		AccountID: accountID,
		httpClient: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: checkSameOriginRedirect,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func checkSameOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errRedirectRefused
	}
	if len(via) == 0 || sameOrigin(via[0].URL, req.URL) {
		return nil
	}
	return errRedirectRefused
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		effectivePort(left) == effectivePort(right)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func (c *Client) apiPath(path string) string {
	return fmt.Sprintf("%s/api/v1/accounts/%d%s", c.BaseURL, c.AccountID, path)
}

func (c *Client) request(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.apiPath(path), body)
	if err != nil {
		return nil, c.safeError(errAPIRequestFailed)
	}

	req.Header.Set("api_access_token", c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func (c *Client) rawRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("%s%s", c.BaseURL, path), body)
	if err != nil {
		return nil, c.safeError(errAPIRequestFailed)
	}

	req.Header.Set("api_access_token", c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func (c *Client) do(req *http.Request, v interface{}) error {
	if c.Verbose {
		c.verbosef("> %s %s", req.Method, req.URL.String())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, errRedirectRefused) {
			return c.safeError(errRedirectRefused)
		}
		return c.safeError(errAPIRequestFailed)
	}
	defer func() { _ = resp.Body.Close() }()

	if c.Verbose {
		c.verbosef("< %s", resp.Status)
	}

	if resp.StatusCode >= 400 {
		return c.apiError(resp)
	}

	if v == nil {
		return nil
	}

	if c.Verbose {
		body, tooLarge, err := readBodyLimited(resp.Body, maxVerboseDecodedResponseBytes)
		if err != nil {
			return c.safeError(errAPIResponseRead)
		}
		if tooLarge {
			return c.safeError(errAPIResponseTooLarge)
		}
		c.verbosef("< %s", redactSensitiveJSON(body, c.APIKey))
		if err := json.Unmarshal(body, v); err != nil {
			return c.safeError(errAPIResponseDecode)
		}
		return nil
	}

	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(v); err != nil {
		return c.safeError(errAPIResponseDecode)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return c.safeError(errAPIResponseDecode)
	}
	return nil
}

func (c *Client) doRaw(req *http.Request) (*RawResponse, error) {
	if c.Verbose {
		c.verbosef("> %s %s", req.Method, req.URL.String())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, errRedirectRefused) {
			return nil, c.safeError(errRedirectRefused)
		}
		return nil, c.safeError(errAPIRequestFailed)
	}
	defer func() { _ = resp.Body.Close() }()

	if c.Verbose {
		c.verbosef("< %s", resp.Status)
	}

	if resp.StatusCode >= 400 {
		return nil, c.apiError(resp)
	}

	// Raw successful responses intentionally retain their existing unbounded
	// contract. Callers explicitly request the complete response bytes.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, c.safeError(errAPIResponseRead)
	}

	return &RawResponse{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header.Clone(),
		Body:       body,
	}, nil
}

func (c *Client) apiError(resp *http.Response) error {
	body, tooLarge, err := readBodyLimited(resp.Body, maxAPIErrorDiagnosticBytes)
	if err != nil {
		return c.safeError(errAPIResponseRead)
	}
	if tooLarge {
		return c.messageError(fmt.Sprintf("API error %d: response body omitted because it exceeds the diagnostic limit", resp.StatusCode))
	}
	return c.messageError(fmt.Sprintf("API error %d: %s", resp.StatusCode, redactSensitiveJSON(body, c.APIKey)))
}

func readBodyLimited(reader io.Reader, limit int64) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > limit {
		return nil, true, nil
	}
	return body, false, nil
}

func redactSensitiveJSON(body []byte, apiKey string) string {
	var value interface{}
	if err := json.Unmarshal(body, &value); err != nil {
		return renderCredentialDiagnostic(string(body), apiKey)
	}

	value = redactSensitiveFields(value, apiKey)

	redacted, err := json.Marshal(value)
	if err != nil {
		return renderCredentialDiagnostic(string(body), apiKey)
	}
	return renderCredentialDiagnostic(string(redacted), apiKey)
}

func redactSensitiveFields(value interface{}, apiKey string) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		redacted := make(map[string]interface{}, len(typed))
		for _, key := range keys {
			child := typed[key]
			redactedKey := renderCredentialDiagnostic(key, apiKey)
			if isSensitiveField(key) {
				redacted[redactedKey] = credentialRedactionMarker(apiKey)
				continue
			}
			// Redacted keys may collide. Sorting makes the lexicographically
			// last original key win deterministically without exposing either key.
			redacted[redactedKey] = redactSensitiveFields(child, apiKey)
		}
		return redacted
	case []interface{}:
		for i, child := range typed {
			typed[i] = redactSensitiveFields(child, apiKey)
		}
	case string:
		return renderCredentialDiagnostic(typed, apiKey)
	}
	return value
}

func renderCredentialDiagnostic(value, apiKey string) string {
	if apiKey == "" {
		return output.SanitizeText(value)
	}
	candidates := []string{apiKey, url.QueryEscape(apiKey), url.PathEscape(apiKey)}
	marker := credentialRedactionMarker(apiKey)
	for _, candidate := range candidates {
		if candidate != "" {
			value = strings.ReplaceAll(value, candidate, marker)
		}
	}
	if strings.ContainsAny(value, "%+") {
		return marker
	}
	return output.SanitizeText(value)
}

func (c *Client) verbosef(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, renderCredentialDiagnostic(message, c.APIKey))
}

func (c *Client) safeError(kind error) error {
	return &credentialSafeError{kind: kind, message: renderCredentialDiagnostic(kind.Error(), c.APIKey)}
}

func (c *Client) messageError(message string) error {
	return errors.New(renderCredentialDiagnostic(message, c.APIKey))
}

func credentialRedactionMarker(apiKey string) string {
	const marker = "[REDACTED]"
	for _, candidate := range []string{apiKey, url.QueryEscape(apiKey), url.PathEscape(apiKey)} {
		if candidate != "" && strings.Contains(marker, candidate) {
			return ""
		}
	}
	return marker
}

func isSensitiveField(key string) bool {
	key = strings.ToLower(key)
	switch key {
	case "access_token", "api_access_token", "pubsub_token", "hmac_identifier":
		return true
	}
	return strings.HasSuffix(key, "_token") ||
		strings.HasSuffix(key, "_secret") ||
		strings.HasSuffix(key, "_key") ||
		strings.HasPrefix(key, "hmac_") ||
		strings.Contains(key, "_hmac_")
}

func (c *Client) Get(path string, params url.Values, v interface{}) error {
	fullPath := path
	if len(params) > 0 {
		fullPath = fmt.Sprintf("%s?%s", path, params.Encode())
	}

	req, err := c.request(http.MethodGet, fullPath, nil)
	if err != nil {
		return err
	}

	return c.do(req, v)
}

// GetRaw makes a GET request to a non-account-scoped path (e.g. /api/v1/profile).
func (c *Client) GetRaw(path string, params url.Values, v interface{}) error {
	fullPath := path
	if len(params) > 0 {
		fullPath = fmt.Sprintf("%s?%s", fullPath, params.Encode())
	}

	req, err := c.rawRequest(http.MethodGet, fullPath, nil)
	if err != nil {
		return err
	}

	return c.do(req, v)
}

// RequestRaw makes an authenticated request and returns the response body without decoding it.
// Account-scoped paths are resolved under /api/v1/accounts/{account_id}; exact paths are
// resolved relative to BaseURL.
func (c *Client) RequestRaw(method, path string, body io.Reader, accountScoped bool, headers http.Header) (*RawResponse, error) {
	var req *http.Request
	var err error
	if accountScoped {
		req, err = c.request(method, path, body)
	} else {
		req, err = c.rawRequest(method, path, body)
	}
	if err != nil {
		return nil, err
	}

	for key, values := range headers {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	return c.doRaw(req)
}

func (c *Client) Post(path string, body io.Reader, v interface{}) error {
	req, err := c.request(http.MethodPost, path, body)
	if err != nil {
		return err
	}

	return c.do(req, v)
}

func (c *Client) Patch(path string, body io.Reader, v interface{}) error {
	req, err := c.request(http.MethodPatch, path, body)
	if err != nil {
		return err
	}

	return c.do(req, v)
}

func (c *Client) Delete(path string, v interface{}) error {
	req, err := c.request(http.MethodDelete, path, nil)
	if err != nil {
		return err
	}

	return c.do(req, v)
}

// Conversations returns the conversations service
func (c *Client) Conversations() *ConversationsService {
	return &ConversationsService{client: c}
}

// Messages returns the messages service for a conversation
func (c *Client) Messages(conversationID int) *MessagesService {
	return &MessagesService{client: c, conversationID: conversationID}
}

// Labels returns the labels service for a conversation
func (c *Client) Labels(conversationID int) *LabelsService {
	return &LabelsService{client: c, conversationID: conversationID}
}

// AccountLabels returns the account-level labels service.
func (c *Client) AccountLabels() *AccountLabelsService {
	return &AccountLabelsService{client: c}
}

// Contacts returns the contacts service
func (c *Client) Contacts() *ContactsService {
	return &ContactsService{client: c}
}

// Inboxes returns the inboxes service
func (c *Client) Inboxes() *InboxesService {
	return &InboxesService{client: c}
}

// Agents returns the agents service
func (c *Client) Agents() *AgentsService {
	return &AgentsService{client: c}
}

// Teams returns the teams service
func (c *Client) Teams() *TeamsService {
	return &TeamsService{client: c}
}

// Profile returns the profile service
func (c *Client) Profile() *ProfileService {
	return &ProfileService{client: c}
}

// HelpCenter returns the help center service.
func (c *Client) HelpCenter() *HelpCenterService {
	return &HelpCenterService{client: c}
}
