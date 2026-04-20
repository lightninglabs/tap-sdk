package rest

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/lightninglabs/tap-sdk/macaroon"
	"google.golang.org/grpc/codes"
)

const (
	// macaroonHeader is the HTTP header name used to pass the macaroon
	// to the gRPC-gateway REST proxy.
	macaroonHeader = "Grpc-Metadata-macaroon"
)

// transport handles HTTP request execution with TLS and macaroon auth.
type transport struct {
	baseURL string
	client  *http.Client
	timeout time.Duration

	macaroons macaroon.Pouch
}

// newTransport creates a configured HTTP transport from the given config
// and macaroon pouch.
func newTransport(cfg *Config, macaroons macaroon.Pouch) (*transport, error) {
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}

	return &transport{
		baseURL:   cfg.BaseURL,
		client:    httpClient,
		timeout:   cfg.timeout(),
		macaroons: macaroons,
	}, nil
}

// apiError is the JSON structure returned by gRPC-gateway on error.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
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
				Details:    apiErr.Details,
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

// grpcCodeFromGateway validates the grpc-gateway error code before exposing
// it through the SDK error surface.
func grpcCodeFromGateway(code int) codes.Code {
	if code < int(codes.OK) || code > int(codes.Unauthenticated) {
		return codes.Unknown
	}

	return codes.Code(code)
}

// newHTTPClient creates an *http.Client with TLS configured per the
// given Config.
func newHTTPClient(cfg *Config) (*http.Client, error) {
	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config: %w", err)
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}, nil
}

// buildTLSConfig creates a *tls.Config from the rest.Config fields.
func buildTLSConfig(cfg *Config) (*tls.Config, error) {
	switch {
	case cfg.TLSPath != "" && cfg.TLSData != "":
		return nil, ErrTLSConflict

	case cfg.Insecure && cfg.SystemCert:
		return nil, ErrInsecureSystemCert

	case cfg.Insecure:
		return &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
		}, nil

	case cfg.SystemCert:
		return &tls.Config{}, nil

	case cfg.TLSData != "":
		block, _ := pem.Decode([]byte(cfg.TLSData))
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf(
				"failed to decode PEM block " +
					"containing tls certificate",
			)
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}

		pool := x509.NewCertPool()
		pool.AddCert(cert)

		return &tls.Config{RootCAs: pool}, nil

	case cfg.TLSPath != "":
		return tlsConfigFromFile(cfg.TLSPath)

	default:
		if _, err := os.Stat(defaultTLSCertPath); err != nil {
			return nil, fmt.Errorf(
				"couldn't find default TLS cert at %s: %v",
				defaultTLSCertPath, err,
			)
		}

		return tlsConfigFromFile(defaultTLSCertPath)
	}
}

// tlsConfigFromFile reads a PEM certificate file and returns a TLS
// config that trusts it.
func tlsConfigFromFile(path string) (*tls.Config, error) {
	certData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't read TLS cert at %s: %v",
			path, err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certData) {
		return nil, fmt.Errorf(
			"failed to add TLS certificate from %s", path,
		)
	}

	return &tls.Config{RootCAs: pool}, nil
}
