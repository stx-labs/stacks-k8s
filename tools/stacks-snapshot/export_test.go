package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestExportPodReadsTheVolumeWithoutPrivileges(t *testing.T) {
	bounds := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
	}
	tainted := []corev1.Toleration{{
		Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "stacks",
		Effect: corev1.TaintEffectNoSchedule,
	}}
	actor := &common.ActorFields{Resources: &bounds, Placement: &common.Placement{Tolerations: &tainted}}
	pod := exportPod(networkUID, "node-01", "data-node-01-0", "busybox:1.36", 1800, actor)
	// Namespaces with a ResourceQuota require bounds on every container, and the volume's node may
	// be tainted: the actor's apply.
	if !reflect.DeepEqual(pod.Spec.Containers[0].Resources, bounds) ||
		!reflect.DeepEqual(pod.Spec.Tolerations, tainted) {
		t.Fatalf("resources %+v, tolerations %+v", pod.Spec.Containers[0].Resources, pod.Spec.Tolerations)
	}
	if bare := exportPod(
		networkUID,
		"node-01",
		"data-node-01-0",
		"busybox:1.36",
		1800,
		nil,
	); bare.Spec.Tolerations != nil ||
		!reflect.DeepEqual(bare.Spec.Containers[0].Resources, corev1.ResourceRequirements{}) {
		t.Fatalf("an actor without bounds or tolerations: %+v", bare.Spec)
	}
	if pod.GenerateName != "snapshot-export-node-01-" || pod.Labels[exportLabel] != networkUID {
		t.Fatalf("identity %+v", pod.ObjectMeta)
	}
	spec := pod.Spec
	if *spec.AutomountServiceAccountToken || *spec.ActiveDeadlineSeconds != 1800 ||
		spec.RestartPolicy != corev1.RestartPolicyNever || spec.NodeName != "" {
		t.Fatalf("pod spec %+v", spec)
	}
	sc := spec.SecurityContext
	if *sc.RunAsUser != actorUser || *sc.RunAsGroup != actorUser || !*sc.RunAsNonRoot ||
		sc.FSGroup != nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatalf("pod security %+v", sc)
	}
	if len(spec.Containers) != 1 || len(spec.Volumes) != 1 {
		t.Fatal("unexpected containers or volumes")
	}
	ctr := spec.Containers[0]
	if ctr.Name != exportContainer || ctr.Image != "busybox:1.36" ||
		strings.Join(ctr.Command, " ") != "sleep 1800" ||
		*ctr.SecurityContext.AllowPrivilegeEscalation || !*ctr.SecurityContext.ReadOnlyRootFilesystem ||
		ctr.SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatalf("container %+v", ctr)
	}
	if m := ctr.VolumeMounts[0]; m.MountPath != dataMount || !m.ReadOnly {
		t.Fatalf("mount %+v", m)
	}
	if v := spec.Volumes[0].PersistentVolumeClaim; v.ClaimName != "data-node-01-0" || !v.ReadOnly {
		t.Fatalf("volume %+v", v)
	}
}

func claim(name, participantUID string, phase corev1.PersistentVolumeClaimPhase) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "lab", Labels: map[string]string{
			api.LabelNetworkUID:     networkUID,
			api.LabelParticipantUID: participantUID,
			api.LabelRole:           api.RoleActor,
		}},
		Status: corev1.PersistentVolumeClaimStatus{Phase: phase},
	}
}

func TestActorClaimSelectsTheOneBoundVolume(t *testing.T) {
	p := participant("node-01", api.ParticipantStacksNode, api.Configuration{})
	p.Spec.NetworkUID = networkUID
	ctx := context.Background()

	c := &cluster{core: fake.NewClientset(
		claim("data-node-01-0", "node-01-uid", corev1.ClaimBound),
		claim("data-signer-01-0", "signer-01-uid", corev1.ClaimBound),
	), namespace: "lab"}
	if name, err := c.actorClaim(ctx, &p); err != nil || name != "data-node-01-0" {
		t.Fatalf("got %s %v", name, err)
	}
	c = &cluster{core: fake.NewClientset(claim("data-node-01-0", "node-01-uid", corev1.ClaimPending)), namespace: "lab"}
	if _, err := c.actorClaim(ctx, &p); err == nil {
		t.Fatal("unbound volume accepted")
	}
	c = &cluster{core: fake.NewClientset(), namespace: "lab"}
	if _, err := c.actorClaim(ctx, &p); err == nil {
		t.Fatal("missing volume accepted")
	}
}

func TestRemoveExportPodsDeletesOnlyThisNetworksLeftovers(t *testing.T) {
	pod := func(name, uid string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "lab", Labels: map[string]string{exportLabel: uid},
		}}
	}
	other := "87654321-1234-1234-1234-123456789abc"
	core := fake.NewClientset(pod("left", networkUID), pod("other", other), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "actor", Namespace: "lab"},
	})
	c := &cluster{core: core, namespace: "lab"}
	if err := c.removeExportPods(context.Background(), networkUID); err != nil {
		t.Fatal(err)
	}
	pods, err := core.CoreV1().Pods("lab").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range pods.Items {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "actor,other" {
		t.Fatalf("remaining %v", names)
	}
}

func TestLimitedBufferKeepsTheStart(t *testing.T) {
	var b limitedBuffer
	n, err := b.Write([]byte(strings.Repeat("x", 5000)))
	if err != nil || n != 5000 || len(b.String()) != 4096 {
		t.Fatalf("n=%d len=%d err=%v", n, len(b.String()), err)
	}
	if n, _ = b.Write([]byte("more")); n != 4 || len(b.String()) != 4096 {
		t.Fatal("buffer grew past its bound")
	}
}

// archiveWithin runs archive against a fake exec stream and fails the test if it does not return
// in time: a hang is exactly the failure being guarded against.
func archiveWithin(t *testing.T, stream streamer) (string, error) {
	t.Helper()
	c := &cluster{streamer: stream}
	type result struct {
		sum string
		err error
	}
	done := make(chan result, 1)
	go func() {
		sum, _, err := c.archive(context.Background(), "export", io.Discard)
		done <- result{sum, err}
	}()
	select {
	case r := <-done:
		return r.sum, r.err
	case <-time.After(10 * time.Second):
		t.Fatal("archive did not return")
		return "", nil
	}
}

func TestArchiveEndsOnTarAndRewriteFailures(t *testing.T) {
	good := tarOf(t, entry{name: "./a", kind: tar.TypeReg, body: "chain"}).Bytes()
	// Complete: the remote shell holds the stream open until the capture acknowledges the marker.
	sum, err := archiveWithin(t, func(_ context.Context, _ string, stdin io.Reader, w io.Writer) error {
		if _, err := w.Write(good); err != nil {
			return err
		}
		_, err := io.Copy(io.Discard, stdin)
		return err
	})
	if err != nil || len(sum) != 64 {
		t.Fatalf("complete stream: %q %v", sum, err)
	}
	// Tar fails part way: the shell exits at once, ending the stream with tar's error.
	_, err = archiveWithin(t, func(_ context.Context, _ string, _ io.Reader, w io.Writer) error {
		_, _ = w.Write(good[:512])
		return errors.New("tar: command terminated with exit code 2: cannot open")
	})
	if err == nil || !strings.Contains(err.Error(), "cannot open") {
		t.Fatalf("tar failure: %v", err)
	}
	// The rewrite refuses a member while the remote keeps streaming. The exec client keeps
	// draining the remote output after a local write error, so only cancelling ends it.
	bad := tarOf(t, entry{name: "./l", link: "x", kind: tar.TypeSymlink}).Bytes()
	_, err = archiveWithin(t, func(ctx context.Context, _ string, _ io.Reader, w io.Writer) error {
		_, _ = w.Write(bad)
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("rewrite failure: %v", err)
	}
}

// TestTarScriptAcknowledgesOnlyASuccessfulTar runs the export Pod's shell script locally, with
// the data mount moved into a temporary directory.
func TestTarScriptAcknowledgesOnlyASuccessfulTar(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar is not available")
	}
	run := func(data string) (*exec.Cmd, io.WriteCloser, *bytes.Buffer) {
		script := strings.Replace(tarScript, dataMount, data, 1)
		// #nosec G204 -- fixed shell and the package's own script; the path is a test directory.
		cmd := exec.CommandContext(t.Context(), "sh", "-c", script)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		cmd.Stdout = &out
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd, stdin, &out
	}
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "chain"), []byte("chain"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd, stdin, out := run(data)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		t.Fatalf("exited before the acknowledgement: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	_ = stdin.Close()
	if err := <-exited; err != nil || out.Len() == 0 {
		t.Fatalf("after the acknowledgement: %v, %d bytes", err, out.Len())
	}
	// A failed tar must not wait for an acknowledgement that never comes.
	cmd, stdin, _ = run(filepath.Join(data, "missing"))
	defer func() { _ = stdin.Close() }()
	exited = make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err == nil {
			t.Fatal("a failed tar exited successfully")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a failed tar waited for the acknowledgement")
	}
}
