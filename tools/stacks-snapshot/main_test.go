package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const networkUID = "12345678-1234-1234-1234-123456789abc"

func validRequest(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"kubeconfig":     "/kubeconfig",
		"context":        "kind-stacks-k8s",
		"namespace":      "lab",
		"network":        "network",
		"networkUID":     networkUID,
		"snapshot":       "epoch40-minimal",
		"image":          "localhost/stacks-snapshots/epoch40-minimal:v1",
		"output":         filepath.Join(t.TempDir(), "out"),
		"timeoutSeconds": 1800,
	}
}

func encode(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(data)
}

func TestReadRequestAppliesDefaultsAndRejectsInvalidInput(t *testing.T) {
	r, err := readRequest(encode(t, validRequest(t)))
	if err != nil {
		t.Fatal(err)
	}
	if r.ExportImage != defaultExportImage || r.BitcoinHeight != 0 {
		t.Fatalf("defaults not applied: %+v", r)
	}
	for name, mutate := range map[string]func(map[string]any){
		"unknown field":    func(m map[string]any) { m["stop"] = true },
		"missing context":  func(m map[string]any) { delete(m, "context") },
		"bad uid":          func(m map[string]any) { m["networkUID"] = "network" },
		"bad namespace":    func(m map[string]any) { m["namespace"] = "Lab" },
		"bad snapshot":     func(m map[string]any) { m["snapshot"] = "a_b" },
		"missing image":    func(m map[string]any) { delete(m, "image") },
		"spaced image":     func(m map[string]any) { m["exportImage"] = "busy box" },
		"relative output":  func(m map[string]any) { m["output"] = "out" },
		"negative height":  func(m map[string]any) { m["bitcoinHeight"] = -1 },
		"short timeout":    func(m map[string]any) { m["timeoutSeconds"] = 59 },
		"long timeout":     func(m map[string]any) { m["timeoutSeconds"] = 86401 },
		"missing timeout":  func(m map[string]any) { delete(m, "timeoutSeconds") },
		"huge bitcoin tip": func(m map[string]any) { m["bitcoinHeight"] = int64(1) << 32 },
	} {
		t.Run(name, func(t *testing.T) {
			m := validRequest(t)
			mutate(m)
			if _, err := readRequest(encode(t, m)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	trailing := append(must(json.Marshal(validRequest(t))), []byte(" {}")...)
	if _, err := readRequest(bytes.NewReader(trailing)); err == nil {
		t.Fatal("accepted a second document")
	}
	if _, err := readRequest(strings.NewReader(strings.Repeat(" ", 65537))); err == nil {
		t.Fatal("accepted an oversized request")
	}
}

func must(data []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return data
}

func TestPrepareOutputCreatesOrAcceptsOnlyAnEmptyDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := prepareOutput(dir); err != nil {
		t.Fatal(err)
	}
	if err := prepareOutput(dir); err != nil {
		t.Fatalf("existing empty directory refused: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.tar.gz"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareOutput(dir); err == nil {
		t.Fatal("non-empty directory accepted")
	}
	file := filepath.Join(dir, "x.tar.gz")
	if err := prepareOutput(file); err == nil {
		t.Fatal("file accepted as output directory")
	}
}

func TestRunRequiresTheCaptureCommand(t *testing.T) {
	for _, args := range [][]string{nil, {"restore"}, {"capture", "extra"}} {
		err := run(context.Background(), args, encode(t, validRequest(t)), &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestValidateAuthRejectsPlugins(t *testing.T) {
	if validateAuth(&rest.Config{BearerToken: "token"}) != nil {
		t.Fatal("static token refused")
	}
	if validateAuth(&rest.Config{ExecProvider: &clientcmdapi.ExecConfig{Command: "login"}}) == nil {
		t.Fatal("exec plugin accepted")
	}
	if validateAuth(&rest.Config{AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "oidc"}}) == nil {
		t.Fatal("auth provider accepted")
	}
}
