// Package rpc implements non-retrying native Bitcoin Core RPC transport.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Credentials supplies HTTP Basic authentication to a Bitcoin Core endpoint.
type Credentials struct {
	// Username identifies the configured RPC principal.
	Username string `json:"username"`
	// Password authenticates the principal; it must never be logged.
	Password string `json:"password"`
}

// Client is a non-retrying, non-redirecting typed Bitcoin Core client.
type Client struct {
	credentials Credentials
	client      *http.Client
}

// New creates a client with bounded connection establishment and no connection reuse.
func New(credentials Credentials) *Client {
	return &Client{credentials: credentials, client: &http.Client{
		Transport: &http.Transport{
			Proxy:             nil,
			DialContext:       (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// ErrInvalidPreflight identifies a definite negative result from a successful preflight read.
var ErrInvalidPreflight = errors.New("invalid production preflight")

// Check verifies regtest and destination validity without creating wallet state.
func (r *Client) Check(ctx context.Context, endpoint, address string) error {
	var chain struct {
		Chain string `json:"chain"`
	}
	if err := r.Call(ctx, endpoint, "preflight-chain", MethodGetBlockchainInfo, []any{}, &chain); err != nil {
		return err
	}
	if chain.Chain == "" {
		return fmt.Errorf("RPC preflight omitted chain identity")
	}
	if chain.Chain != ChainRegtest {
		return fmt.Errorf("%w: target is not regtest", ErrInvalidPreflight)
	}
	var validation struct {
		Valid *bool `json:"isvalid"`
	}
	if err := r.Call(
		ctx,
		endpoint,
		"preflight-address",
		MethodValidateAddress,
		[]any{address},
		&validation,
	); err != nil {
		return err
	}
	if validation.Valid == nil {
		return fmt.Errorf("RPC preflight omitted destination validity")
	}
	if !*validation.Valid {
		return fmt.Errorf("%w: invalid regtest destination", ErrInvalidPreflight)
	}
	return nil
}

// MaxGeneratedBlocks bounds one generation request so its hash list fits the response bound.
const MaxGeneratedBlocks = 500

// Generate requests exactly one block and requires an attributable success receipt.
func (r *Client) Generate(ctx context.Context, endpoint, address, id string) (string, error) {
	return r.GenerateBlocks(ctx, endpoint, address, id, 1)
}

// GenerateBlocks requests count blocks in one call and returns the last block hash only when
// the receipt lists exactly count valid hashes. Any error leaves the number of generated blocks unknown.
func (r *Client) GenerateBlocks(ctx context.Context, endpoint, address, id string, count int) (string, error) {
	if count < 1 || count > MaxGeneratedBlocks {
		return "", &CallError{Kind: FailureRequest, message: "unsupported generation count"}
	}
	var hashes []string
	if err := r.Call(ctx, endpoint, id, MethodGenerateToAddress, []any{
		count,
		address,
		1000000,
	}, &hashes); err != nil {
		return "", err
	}
	if len(hashes) != count {
		return "", fmt.Errorf("incomplete generation receipt")
	}
	for _, hash := range hashes {
		if len(hash) != 64 {
			return "", fmt.Errorf("incomplete generation receipt")
		}
		for _, c := range hash {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return "", fmt.Errorf("invalid block hash")
			}
		}
	}
	return hashes[count-1], nil
}

const (
	// maxReceiptBytes bounds one Call response.
	maxReceiptBytes = 65536
	// MaxBatchRequests bounds one Batch, and therefore its response budget.
	MaxBatchRequests = 100
	// batchReceiptBytesPerRequest budgets each batched receipt; a verbose gettxout
	// receipt is well under 1 KiB.
	batchReceiptBytesPerRequest = 4096
)

// Request is one named call within a Batch.
type Request struct {
	// ID must be non-empty and unique within its batch.
	ID string
	// Method is the native RPC method.
	Method string
	// Params are the positional parameters; nil sends an empty list.
	Params []any
	// Result receives the decoded result.
	Result any
}

// receipt is one JSON-RPC response object.
type receipt struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// failed reports a non-null error member.
func (r receipt) failed() bool { return len(r.Error) != 0 && string(r.Error) != "null" }

// Call avoids replayable request bodies and bounds response memory; errors omit server response bodies.
func (r *Client) Call(ctx context.Context, endpoint, id, method string, params []any, result any) error {
	data, err := r.post(ctx, endpoint, envelope(id, method, params), maxReceiptBytes)
	if err != nil {
		return err
	}
	var response receipt
	if err := json.Unmarshal(data, &response); err != nil || response.ID != id || response.failed() {
		return &CallError{Kind: FailureResponse, message: "RPC did not return a matching success receipt"}
	}
	return decode(response.Result, result)
}

// Batch sends requests in one HTTP exchange with Call's transport guarantees. It succeeds
// only when every request has exactly one matching success receipt, in any order. Bitcoin
// Core executes batched requests individually: a batch is neither atomic nor a consistent
// snapshot. Results are unspecified after an error. An empty batch sends nothing.
func (r *Client) Batch(ctx context.Context, endpoint string, requests []Request) error {
	if len(requests) == 0 {
		return nil
	}
	if len(requests) > MaxBatchRequests {
		return &CallError{Kind: FailureRequest, message: "unsupported batch size"}
	}
	payload := make([]map[string]any, len(requests))
	pending := make(map[string]int, len(requests))
	for i, request := range requests {
		if _, duplicate := pending[request.ID]; request.ID == "" || duplicate {
			return &CallError{Kind: FailureRequest, message: "batch request IDs must be unique"}
		}
		pending[request.ID] = i
		payload[i] = envelope(request.ID, request.Method, request.Params)
	}
	data, err := r.post(ctx, endpoint, payload, max(maxReceiptBytes, len(requests)*batchReceiptBytesPerRequest))
	if err != nil {
		return err
	}
	var responses []receipt
	if err := json.Unmarshal(data, &responses); err != nil || len(responses) != len(requests) {
		return &CallError{Kind: FailureResponse, message: "RPC did not return a matching batch receipt"}
	}
	for _, response := range responses {
		i, ok := pending[response.ID]
		if !ok || response.failed() {
			return &CallError{Kind: FailureResponse, message: "RPC did not return a matching batch receipt"}
		}
		delete(pending, response.ID) // A repeated ID no longer matches.
		if err := decode(response.Result, requests[i].Result); err != nil {
			return err
		}
	}
	return nil
}

// envelope builds one JSON-RPC 2.0 request object.
func envelope(id, method string, params []any) map[string]any {
	if params == nil {
		params = []any{}
	}
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}

// decode unmarshals one success result.
func decode(data json.RawMessage, result any) error {
	if err := json.Unmarshal(data, result); err != nil {
		return &CallError{Kind: FailureResponse, message: "RPC result could not be decoded"}
	}
	return nil
}

// post sends one non-replayable JSON payload and returns at most limit response bytes.
func (r *Client) post(ctx context.Context, endpoint string, payload any, limit int) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &CallError{Kind: FailureRequest, message: "encode RPC request"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return nil, &CallError{Kind: FailureRequest, message: "create RPC request"}
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(r.credentials.Username, r.credentials.Password)
	response, err := r.client.Do(request)
	if err != nil {
		return nil, &CallError{Kind: FailureTransport, message: "RPC transport did not yield a receipt"}
	}
	defer func() { _ = response.Body.Close() }() // Read/cleanup completion cannot change the operation's result.
	if response.StatusCode != http.StatusOK {
		return nil, &CallError{Kind: FailureResponse, message: fmt.Sprintf("RPC HTTP status %d", response.StatusCode)}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil {
		return nil, &CallError{Kind: FailureTransport, message: "RPC receipt unreadable or oversized"}
	}
	if len(data) > limit {
		return nil, &CallError{Kind: FailureBound, message: "RPC receipt unreadable or oversized"}
	}
	return data, nil
}
