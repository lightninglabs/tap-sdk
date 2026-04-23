package rest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/lightninglabs/tap-sdk/macaroon"
	"google.golang.org/grpc/codes"
)

const (
	// macaroonHeader is the canonical HTTP header name used to pass
	// the macaroon to the gRPC-gateway REST proxy.
	macaroonHeader = "Grpc-Metadata-Macaroon"
)

// transport handles HTTP request execution with TLS and macaroon auth.
type transport struct {
	baseURL string
	client  *http.Client
	timeout time.Duration

	// tlsCfg is also used to build a WebSocket dialer for event
	// subscription streams, so we keep a shared reference instead of
	// rebuilding it from cfg.
	tlsCfg *tls.Config

	macaroons macaroon.Pouch
}

// newTransport creates a configured HTTP transport from the given config
// and macaroon pouch.
func newTransport(cfg *Config, macaroons macaroon.Pouch) (*transport, error) {
	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config: %w", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}

	return &transport{
		baseURL:   cfg.BaseURL,
		client:    httpClient,
		timeout:   cfg.timeout(),
		tlsCfg:    tlsCfg,
		macaroons: macaroons,
	}, nil
}

// apiError is the JSON structure returned by gRPC-gateway on error.
// Details is typed as RawMessage because tapd emits it as a JSON
// array (the default grpc-gateway shape) — a plain string field would
// fail to unmarshal and send us down the opaque fallback path, losing
// the embedded gRPC code.
type apiError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

// doGet performs an authenticated GET request and decodes the
// JSON body into result.
func (t *transport) doGet(ctx context.Context, path string,
	mac macaroon.TaprpcServiceMac, result any) error {

	return t.do(
		ctx, http.MethodGet, path, mac, nil, result,
	)
}

// doPost performs an authenticated POST request with a JSON body
// and decodes the JSON response into result.
func (t *transport) doPost(ctx context.Context, path string,
	mac macaroon.TaprpcServiceMac, body, result any) error {

	return t.do(
		ctx, http.MethodPost, path, mac, body, result,
	)
}

// do executes an HTTP request with the given method, path, optional
// JSON body, and decodes the response into result.
func (t *transport) do(ctx context.Context, method, path string,
	mac macaroon.TaprpcServiceMac, body, result any) error {

	reqCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	url := t.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w",
				err)
		}
		bodyReader = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequestWithContext(
		reqCtx, method, url, bodyReader,
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header[macaroonHeader] = []string{
		string(t.macaroons[mac]),
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiError
		if json.Unmarshal(respBody, &apiErr) == nil &&
			apiErr.Message != "" {

			return &APIError{
				StatusCode: resp.StatusCode,
				GRPCCode:   grpcCodeFromGateway(apiErr.Code),
				Message:    apiErr.Message,
				Details:    detailsString(apiErr.Details),
			}
		}

		return &APIError{
			StatusCode: resp.StatusCode,
			GRPCCode:   codes.Unknown,
			Message:    string(respBody),
		}
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w",
				err)
		}
	}

	return nil
}

// detailsString renders the grpc-gateway details payload as a string
// for the public APIError surface. Empty, null, or empty-array details
// collapse to "" so Error() omits the parenthetical.
func detailsString(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}

	switch string(trimmed) {
	case "null", "[]", `""`:
		return ""
	}

	return string(trimmed)
}

// grpcCodeFromGateway validates the grpc-gateway error code before exposing
// it through the SDK error surface.
func grpcCodeFromGateway(code int) codes.Code {
	if code < int(codes.OK) || code > int(codes.Unauthenticated) {
		return codes.Unknown
	}

	return codes.Code(code)
}

// buildTLSConfig creates a *tls.Config from the rest.Config's TLS
// source, falling back to tapd's default tls.cert path when unset.
func buildTLSConfig(cfg *Config) (*tls.Config, error) {
	source := cfg.TLS
	if source == nil {
		if _, err := os.Stat(defaultTLSCertPath); err != nil {
			return nil, fmt.Errorf(
				"couldn't find default TLS cert at %s: %v",
				defaultTLSCertPath, err,
			)
		}
		source = TLSFromPath(defaultTLSCertPath)
	}

	return source.tlsConfig()
}
