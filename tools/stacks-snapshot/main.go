// Command stacks-snapshot captures an initialized network's actor data as a restorable chainstate snapshot.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// defaultExportImage provides tar for the export Pod and the tools of the restore script as the
// snapshot image base; it is pinned by digest.
const defaultExportImage = "busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662"

// request selects the cluster, the exact source network and where the snapshot is written.
type request struct {
	Kubeconfig     string `json:"kubeconfig"`
	Context        string `json:"context"`
	Namespace      string `json:"namespace"`
	Network        string `json:"network"`
	NetworkUID     string `json:"networkUID"`
	Snapshot       string `json:"snapshot"`
	Image          string `json:"image"`
	ExportImage    string `json:"exportImage,omitempty"`
	Output         string `json:"output"`
	BitcoinHeight  int64  `json:"bitcoinHeight,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	// Resume exports a network an earlier capture already stopped, with the tips it recorded.
	Resume bool `json:"resume,omitempty"`
}

var (
	namePattern  = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	uidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	imagePattern = regexp.MustCompile(`^[[:graph:]]{1,512}$`)
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stacks-snapshot:", err)
		os.Exit(1)
	}
}

// run captures one snapshot. Progress goes to logs; the report is the only stdout content.
func run(ctx context.Context, args []string, input io.Reader, output, logs io.Writer) error {
	if len(args) != 1 || args[0] != "capture" {
		return errors.New("usage: stacks-snapshot capture < request.json > report.json")
	}
	r, err := readRequest(input)
	if err != nil {
		return err
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: r.Kubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: r.Context}).ClientConfig()
	if err != nil {
		return errors.New("could not load selected kube-context")
	}
	if err = validateAuth(config); err != nil {
		return err
	}
	c, err := newCluster(config, r.Namespace)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.TimeoutSeconds)*time.Second)
	defer cancel()
	progress := func(message string) {
		_, _ = fmt.Fprintf(logs, "%s stacks-snapshot: %s\n", time.Now().UTC().Format(time.RFC3339), message)
	}
	result, err := capture(ctx, c, r, progress)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}

// readRequest accepts exactly one bounded JSON request and applies defaults.
func readRequest(input io.Reader) (request, error) {
	var r request
	data, err := io.ReadAll(io.LimitReader(input, 65537))
	if err != nil || len(data) > 65536 {
		return r, errors.New("request exceeds byte bound or cannot be read")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil || d.Decode(&struct{}{}) != io.EOF {
		return r, errors.New("expected one bounded request on stdin")
	}
	if r.ExportImage == "" {
		r.ExportImage = defaultExportImage
	}
	return r, r.validate()
}

func (r request) validate() error {
	if r.Kubeconfig == "" || r.Context == "" || !uidPattern.MatchString(r.NetworkUID) {
		return errors.New("invalid cluster or network identity")
	}
	for _, name := range []string{r.Namespace, r.Network, r.Snapshot} {
		if !namePattern.MatchString(name) {
			return errors.New("invalid resource name")
		}
	}
	if !imagePattern.MatchString(r.Image) || !imagePattern.MatchString(r.ExportImage) {
		return errors.New("invalid image reference")
	}
	if !filepath.IsAbs(r.Output) {
		return errors.New("output must be an absolute directory path")
	}
	if r.BitcoinHeight < 0 || r.BitcoinHeight > 4294967295 {
		return errors.New("invalid Bitcoin height")
	}
	if r.Resume && r.BitcoinHeight > 0 {
		return errors.New("bitcoinHeight does not apply to a resumed export")
	}
	if r.TimeoutSeconds < 60 || r.TimeoutSeconds > 86400 {
		return errors.New("timeoutSeconds must be between 60 and 86400")
	}
	return nil
}

// prepareOutput creates the output directory or accepts an existing empty one, so archives
// from two captures never mix.
func prepareOutput(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err = os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("cannot create output directory: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("cannot read output directory: %w", err)
	case len(entries) > 0:
		return errors.New("output directory is not empty")
	}
	return nil
}

// validateAuth avoids unbounded external credential execution in this bounded CLI.
func validateAuth(config *rest.Config) error {
	if config.ExecProvider != nil || config.AuthProvider != nil {
		return errors.New("capture requires static certificate/token credentials; auth plugins are unsupported")
	}
	return nil
}
