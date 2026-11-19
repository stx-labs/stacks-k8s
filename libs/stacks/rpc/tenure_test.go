package rpc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestNakamotoTipRequiresNativeHeaderAndMatchingIdentity(t *testing.T) {
	view := ChainView{
		Info:          Info{StacksHeight: 3},
		ConsensusHash: strings.Repeat("a", 40),
		IndexBlockID:  strings.Repeat("b", 64),
	}
	valid := fmt.Sprintf(`{"anchored_header":{"Nakamoto":{"chain_length":3,"consensus_hash":%q}}}`, view.ConsensusHash)
	for _, tc := range []struct {
		name, body    string
		code          int
		want, wantErr bool
	}{
		{"Nakamoto", valid, 200, true, false},
		{"legacy", `{"anchored_header":{"Epoch2":{"version":0}}}`, 200, false, false},
		{"missing", `{}`, 200, false, true},
		{"null", `{"anchored_header":{"Nakamoto":null}}`, 200, false, true},
		{"height absent", strings.Replace(valid, `"chain_length":3,`, "", 1), 200, false, true},
		{"height differs", strings.Replace(valid, `"chain_length":3`, `"chain_length":4`, 1), 200, false, true},
		{
			"consensus differs",
			strings.ReplaceAll(valid, view.ConsensusHash, strings.Repeat("c", 40)),
			200,
			false,
			true,
		},
		{"ambiguous family", `{"anchored_header":{"Epoch2":{},"Nakamoto":{}}}`, 200, false, true},
		{"unknown family", `{"anchored_header":{"Future":{}}}`, 200, false, true},
		{"missing endpoint", valid, 404, false, true},
		{"malformed", `{`, 200, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/v3/tenures/tip_metadata/"+view.ConsensusHash {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.code)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			got, err := c.NakamotoTip(context.Background(), view)
			if got != tc.want || (err != nil) != tc.wantErr || calls != 1 {
				t.Fatalf("got %v, %v, calls=%d", got, err, calls)
			}
			bad := view
			bad.ConsensusHash = "../other"
			if ok, err := c.NakamotoTip(context.Background(), bad); ok || err == nil || calls != 1 {
				t.Fatal("invalid identity reached transport")
			}
		})
	}
}

func TestTenureSortitionRequiresTheRequestedSortition(t *testing.T) {
	consensus := strings.Repeat("a", 40)
	burn := strings.Repeat("b", 64)
	valid := fmt.Sprintf(
		`[{"burn_block_hash":"0x%s","burn_block_height":252,"consensus_hash":"0x%s"}]`,
		burn,
		consensus,
	)
	for _, tc := range []struct {
		name, body string
		code       int
		wantErr    bool
	}{
		{"exact", valid, 200, false},
		{"empty", `[]`, 200, true},
		{"several", "[" + valid[1:len(valid)-1] + "," + valid[1:len(valid)-1] + "]", 200, true},
		{"other tenure", strings.Replace(valid, "0x"+consensus, "0x"+strings.Repeat("c", 40), 1), 200, true},
		{"height absent", strings.Replace(valid, `"burn_block_height":252,`, "", 1), 200, true},
		{"unprefixed hash", strings.Replace(valid, "0x"+burn, burn, 1), 200, true},
		{"short hash", strings.Replace(valid, burn, burn[2:], 1), 200, true},
		{"upper-case hash", strings.Replace(valid, burn, strings.ToUpper(burn), 1), 200, true},
		{"missing", valid, 404, true},
		{"malformed", `{`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.URL.Path != "/v3/sortitions/consensus/"+consensus {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.code)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			got, err := c.TenureSortition(context.Background(), consensus)
			if (err != nil) != tc.wantErr || calls != 1 {
				t.Fatalf("got %+v, %v, calls=%d", got, err, calls)
			}
			if !tc.wantErr && got != (TenureSortition{ConsensusHash: consensus, BurnBlockHash: burn, BurnHeight: 252}) {
				t.Fatalf("got %+v", got)
			}
			for _, bad := range []string{"../other", strings.Repeat("0", 40), strings.ToUpper(consensus)} {
				if _, err := c.TenureSortition(context.Background(), bad); err == nil || calls != 1 {
					t.Fatalf("invalid consensus hash %q reached transport", bad)
				}
			}
		})
	}
}
