package rqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config contains the rqlite driver configuration.
type Config struct {
	// URL is the base HTTP address of the target rqlite node.
	URL string
	// Timeout bounds each outbound HTTP request to rqlite.
	Timeout time.Duration
	// Username is the optional HTTP basic auth username.
	Username string
	// Password is the optional HTTP basic auth password.
	Password string
	// Headers carries additional HTTP headers sent with every request.
	Headers map[string]string
}

// Driver executes migration operations against rqlite over HTTP.
type Driver struct {
	baseURL  *url.URL
	client   *http.Client
	username string
	password string
	headers  map[string]string
	timeout  time.Duration
}

type response struct {
	Results []result `json:"results"`
}

type result struct {
	Error string              `json:"error"`
	Rows  []map[string]any    `json:"rows"`
	Data  map[string]any      `json:"data"`
	Types map[string]string   `json:"types"`
	Extra []map[string]string `json:"extra"`
}

// New creates a new rqlite HTTP driver.
func New(config Config) (*Driver, error) {
	if strings.TrimSpace(config.URL) == "" {
		return nil, fmt.Errorf("rqlite URL must not be empty")
	}

	baseURL, err := url.Parse(config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse rqlite URL %q: %w", config.URL, err)
	}

	return &Driver{
		baseURL:  baseURL,
		client:   &http.Client{Timeout: config.Timeout},
		username: config.Username,
		password: config.Password,
		headers:  cloneHeaders(config.Headers),
		timeout:  config.Timeout,
	}, nil
}

// Exec sends write statements to rqlite.
func (driver *Driver) Exec(ctx context.Context, statements []string, transactional bool) error {
	if len(statements) == 0 {
		return nil
	}

	body, err := json.Marshal(statements)
	if err != nil {
		return fmt.Errorf("encode execute statements: %w", err)
	}

	endpoint := driver.endpoint("/db/execute", map[string]string{
		"transaction": boolQueryValue(transactional),
		"timeout":     durationQueryValue(driver.timeout),
	})

	responseBody, err := driver.doJSONRequest(ctx, endpoint, body)
	if err != nil {
		return err
	}

	decoded, err := decodeResponse(responseBody)
	if err != nil {
		return err
	}

	for _, result := range decoded.Results {
		if result.Error != "" {
			return fmt.Errorf("rqlite execute error: %s", result.Error)
		}
	}

	return nil
}

// Query sends a read statement to rqlite and returns associative rows.
func (driver *Driver) Query(ctx context.Context, statement string) ([]map[string]any, error) {
	body, err := json.Marshal([]string{statement})
	if err != nil {
		return nil, fmt.Errorf("encode query statement: %w", err)
	}

	endpoint := driver.endpoint("/db/query", map[string]string{
		"associative": "true",
		"timeout":     durationQueryValue(driver.timeout),
	})

	responseBody, err := driver.doJSONRequest(ctx, endpoint, body)
	if err != nil {
		return nil, err
	}

	decoded, err := decodeResponse(responseBody)
	if err != nil {
		return nil, err
	}
	if len(decoded.Results) == 0 {
		return nil, nil
	}

	if decoded.Results[0].Error != "" {
		return nil, fmt.Errorf("rqlite query error: %s", decoded.Results[0].Error)
	}

	return decoded.Results[0].Rows, nil
}

// Close releases driver resources.
func (driver *Driver) Close() error {
	return nil
}

func (driver *Driver) endpoint(path string, values map[string]string) string {
	resolved := *driver.baseURL
	resolved.Path = strings.TrimRight(driver.baseURL.Path, "/") + path

	query := resolved.Query()
	for key, value := range values {
		if value == "" {
			continue
		}
		query.Set(key, value)
	}
	resolved.RawQuery = query.Encode()

	return resolved.String()
}

func (driver *Driver) doJSONRequest(
	ctx context.Context,
	endpoint string,
	body []byte,
) ([]byte, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("build rqlite request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	for key, value := range driver.headers {
		request.Header.Set(key, value)
	}
	if driver.username != "" || driver.password != "" {
		request.SetBasicAuth(driver.username, driver.password)
	}

	response, err := driver.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send rqlite request: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read rqlite response: %w", err)
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf(
			"unexpected HTTP status %d from rqlite: %s",
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}

	return responseBody, nil
}

func decodeResponse(body []byte) (response, error) {
	var decoded response
	if err := json.Unmarshal(body, &decoded); err != nil {
		return response{}, fmt.Errorf("decode rqlite response: %w", err)
	}
	return decoded, nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(headers))
	for key, value := range headers {
		cloned[key] = value
	}

	return cloned
}

func boolQueryValue(enabled bool) string {
	if !enabled {
		return ""
	}
	return "true"
}

func durationQueryValue(timeout time.Duration) string {
	if timeout <= 0 {
		return ""
	}
	return timeout.String()
}
