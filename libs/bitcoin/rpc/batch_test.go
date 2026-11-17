package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// batchServer answers every batch with respond(decoded requests) and counts exchanges.
func batchServer(t *testing.T, respond func([]map[string]any) string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var exchanges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		var requests []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&requests); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, respond(requests))
	}))
	t.Cleanup(server.Close)
	return server, &exchanges
}

// txOutReceipt is a verbose Bitcoin Core gettxout receipt for a P2PKH coinbase output.
func txOutReceipt(id string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"result":{"bestblock":"%064x","confirmations":250,`+
		`"value":50.00000000,"scriptPubKey":{"asm":"OP_DUP OP_HASH160 %040x OP_EQUALVERIFY OP_CHECKSIG",`+
		`"desc":"addr(mrCDrCybB6J1vRfbwM5hemdJz73FwDBC8r)#8xqcgfsv","hex":"76a914%040x88ac",`+
		`"address":"mrCDrCybB6J1vRfbwM5hemdJz73FwDBC8r","type":"pubkeyhash"},"coinbase":true}}`, id, 1, 2, 2)
}

func TestBatchSendsOneExchangeAndMatchesReceiptsByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		var requests []struct {
			Version string `json:"jsonrpc"`
			ID      string `json:"id"`
			Method  string `json:"method"`
			Params  []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requests); err != nil {
			t.Error(err)
		}
		if !ok || user != "observer" || password != "private" || len(requests) != 2 ||
			requests[0].Version != "2.0" || requests[0].ID != "a" || requests[0].Method != "gettxout" ||
			len(requests[0].Params) != 3 || requests[1].ID != "b" || requests[1].Params == nil {
			t.Error("wire contract changed", requests)
		}
		// Receipts may arrive in any order.
		_, _ = io.WriteString(w, `[{"id":"b","result":{"n":2}},{"id":"a","result":{"n":1},"error":null}]`)
	}))
	defer server.Close()
	var a, b struct {
		N int `json:"n"`
	}
	err := New(Credentials{Username: "observer", Password: "private"}).Batch(t.Context(), server.URL, []Request{
		{ID: "a", Method: "gettxout", Params: []any{"txid", 0, true}, Result: &a},
		{ID: "b", Method: "getblockchaininfo", Result: &b},
	})
	if err != nil || a.N != 1 || b.N != 2 {
		t.Fatal(err, a, b)
	}
}

func TestBatchRejectsUnmatchedReceipts(t *testing.T) {
	for name, body := range map[string]string{
		"missing":   `[{"id":"a","result":1}]`,
		"duplicate": `[{"id":"a","result":1},{"id":"a","result":1}]`,
		"unknown":   `[{"id":"a","result":1},{"id":"c","result":1}]`,
		"error":     `[{"id":"a","result":1},{"id":"b","result":null,"error":{"code":-1,"message":"x"}}]`,
		"result":    `[{"id":"a","result":1},{"id":"b"}]`,
		"object":    `{"id":"a","result":1}`,
		"extra":     `[{"id":"a","result":1},{"id":"b","result":1},{"id":"c","result":1}]`,
	} {
		t.Run(name, func(t *testing.T) {
			server, _ := batchServer(t, func([]map[string]any) string { return body })
			var a, b int
			err := New(Credentials{}).Batch(t.Context(), server.URL, []Request{
				{ID: "a", Method: "m", Result: &a},
				{ID: "b", Method: "m", Result: &b},
			})
			var callErr *CallError
			if !errors.As(err, &callErr) || callErr.Kind != FailureResponse {
				t.Fatal("unmatched batch receipt accepted", err)
			}
		})
	}
}

func TestBatchRejectsInvalidRequestsWithoutSending(t *testing.T) {
	oversized := make([]Request, MaxBatchRequests+1)
	for i := range oversized {
		oversized[i] = Request{ID: fmt.Sprint(i), Method: "m"}
	}
	for name, requests := range map[string][]Request{
		"oversized": oversized,
		"duplicate": {{ID: "a", Method: "m"}, {ID: "a", Method: "m"}},
		"empty ID":  {{ID: "", Method: "m"}},
	} {
		t.Run(name, func(t *testing.T) {
			server, exchanges := batchServer(t, func([]map[string]any) string { return "[]" })
			err := New(Credentials{}).Batch(t.Context(), server.URL, requests)
			var callErr *CallError
			if !errors.As(err, &callErr) || callErr.Kind != FailureRequest || exchanges.Load() != 0 {
				t.Fatal("invalid batch was sent or accepted", err, exchanges.Load())
			}
		})
	}
	server, exchanges := batchServer(t, func([]map[string]any) string { return "[]" })
	if err := New(Credentials{}).Batch(t.Context(), server.URL, nil); err != nil || exchanges.Load() != 0 {
		t.Fatal("empty batch was sent or rejected", err, exchanges.Load())
	}
}

func TestBatchMaximumTxOutReceiptsFitResponseBound(t *testing.T) {
	if size := len(txOutReceipt("coin-99")); size > batchReceiptBytesPerRequest/4 {
		t.Fatalf("gettxout receipt fixture is %d bytes; per-request budget lacks headroom", size)
	}
	server, exchanges := batchServer(t, func(requests []map[string]any) string {
		receipts := make([]string, len(requests))
		for i, request := range requests {
			receipts[i] = txOutReceipt(request["id"].(string))
		}
		return "[" + strings.Join(receipts, ",") + "]"
	})
	requests := make([]Request, MaxBatchRequests)
	coins := make([]struct {
		Coinbase *bool `json:"coinbase"`
	}, MaxBatchRequests)
	for i := range requests {
		requests[i] = Request{ID: fmt.Sprintf("coin-%d", i), Method: MethodGetTxOut, Result: &coins[i]}
	}
	if err := New(Credentials{}).Batch(t.Context(), server.URL, requests); err != nil || exchanges.Load() != 1 {
		t.Fatal(err, exchanges.Load())
	}
	for i, coin := range coins {
		if coin.Coinbase == nil || !*coin.Coinbase {
			t.Fatal("receipt not decoded", i)
		}
	}
}

func TestBatchBoundsResponseMemory(t *testing.T) {
	server, _ := batchServer(t, func([]map[string]any) string {
		return `[{"id":"a","result":"` + strings.Repeat("x", maxReceiptBytes) + `"}]`
	})
	var result string
	err := New(Credentials{}).Batch(t.Context(), server.URL, []Request{{ID: "a", Method: "m", Result: &result}})
	var callErr *CallError
	if !errors.As(err, &callErr) || callErr.Kind != FailureBound {
		t.Fatal("oversized batch receipt accepted", err)
	}
}
