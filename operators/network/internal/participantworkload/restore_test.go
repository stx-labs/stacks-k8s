package participantworkload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func restoreFixture() *chainstateRestore {
	return &chainstateRestore{
		image:   "snapshot:test",
		archive: api.SnapshotArchive{Path: "node-01.tar.gz", SHA256: strings.Repeat("a", 64), Image: "stacks:test"},
	}
}

func TestRestoreInitPrecedesActorChecks(t *testing.T) {
	for _, kind := range []api.ParticipantKind{"BitcoinNode", "StacksNode", "StacksSigner"} {
		t.Run(string(kind), func(t *testing.T) {
			p := participantFixture()
			if kind != "BitcoinNode" {
				p = stacksParticipantFixture(kind)
			}
			plain, err := actorWorkload(p, nativeRuntimeFixture(), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range plain.Spec.Template.Spec.InitContainers {
				if c.Name == restoreContainer {
					t.Fatal("a network initialized from genesis must not restore data")
				}
			}
			restored, err := actorWorkload(p, nativeRuntimeFixture(), restoreFixture())
			if err != nil {
				t.Fatal(err)
			}
			pod := restored.Spec.Template.Spec
			if len(pod.InitContainers) != len(plain.Spec.Template.Spec.InitContainers)+1 {
				t.Fatalf("init containers: %+v", pod.InitContainers)
			}
			init := pod.InitContainers[0]
			args := init.Command[len(init.Command)-2:]
			if init.Name != restoreContainer || init.Image != "snapshot:test" ||
				args[0] != "node-01.tar.gz" || args[1] != strings.Repeat("a", 64) {
				t.Fatalf("restore must run first with the selected archive: %+v", init)
			}
			if !ptr.Deref(init.SecurityContext.ReadOnlyRootFilesystem, false) ||
				ptr.Deref(init.SecurityContext.AllowPrivilegeEscalation, true) ||
				len(init.Env) != 0 || len(init.EnvFrom) != 0 ||
				len(init.VolumeMounts) != 1 || init.VolumeMounts[0].Name != "data" ||
				init.VolumeMounts[0].MountPath != "/data" {
				t.Fatalf("restore must see only the data volume, without credentials: %+v", init)
			}
			// Namespaces with a ResourceQuota require bounds on every container.
			if !reflect.DeepEqual(init.Resources, pod.Containers[0].Resources) {
				t.Fatalf("restore resources %+v, actor %+v", init.Resources, pod.Containers[0].Resources)
			}
		})
	}
}

// The archive comes from the frozen genesis alone, by participant UID: no snapshot resource is
// read, so deleting or recreating the snapshot after freeze cannot affect a restored network.
func TestChainstateRestoreForSelectsRestoredMembersOnly(t *testing.T) {
	archive := restoreFixture().archive
	restored := &api.StacksGenesis{Spec: api.StacksGenesisSpec{
		Source: api.GenesisSource{Restored: &api.RestoredChainstate{Image: "snapshot:test"}},
		Bootstrap: api.Bootstrap{Requirements: []api.BootstrapRequirement{
			{Kind: "StacksNode", Participant: common.Binding{UID: "node-uid"}, RestoredArchive: &archive},
			{Kind: "StacksStacker", Participant: common.Binding{UID: "stacker-uid"}},
		}},
	}}
	fromGenesis := restored.DeepCopy()
	fromGenesis.Spec.Source.Restored = nil
	withUID := func(name, uid string) *api.StacksNetworkParticipant {
		p := stacksParticipantFixture("StacksNode")
		p.Spec.ParticipantName, p.UID = name, types.UID(uid)
		return p
	}
	for name, tc := range map[string]struct {
		genesis *api.StacksGenesis
		p       *api.StacksNetworkParticipant
		archive bool
	}{
		"restored member":  {restored, withUID("node-01", "node-uid"), true},
		"late participant": {restored, withUID("node-02", "late-uid"), false},
		// A replacement reuses the name but is a new instance, which starts empty.
		"replaced member": {restored, withUID("node-01", "replacement-uid"), false},
		"genesis network": {fromGenesis, withUID("node-01", "node-uid"), false},
	} {
		t.Run(name, func(t *testing.T) {
			restore := chainstateRestoreFor(tc.genesis, tc.p)
			if (restore != nil) != tc.archive {
				t.Fatalf("restore %+v", restore)
			}
			if restore != nil && (restore.image != "snapshot:test" || restore.archive != archive) {
				t.Fatalf("wrong archive: %+v", restore)
			}
		})
	}
}

func TestRestoreImageUnavailableReportsPullFailures(t *testing.T) {
	waiting := func(name, reason string) *corev1.Pod {
		return &corev1.Pod{Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{{
			Name:  name,
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}},
		}}}}
	}
	for _, reason := range []string{"ErrImagePull", "ImagePullBackOff", "InvalidImageName"} {
		if !restoreImageUnavailable(waiting(restoreContainer, reason)) {
			t.Fatalf("%s not reported", reason)
		}
	}
	if restoreImageUnavailable(waiting(restoreContainer, "PodInitializing")) ||
		restoreImageUnavailable(waiting("config-check", "ImagePullBackOff")) {
		t.Fatal("only the restore container's pull failures are restore failures")
	}
}

func TestInitContainerFailedDistinguishesExitCodes(t *testing.T) {
	terminated := func(code int32) corev1.ContainerState {
		return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: code}}
	}
	waiting := corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
	for name, tc := range map[string]struct {
		status corev1.ContainerStatus
		failed bool
	}{
		"succeeded": {corev1.ContainerStatus{Name: restoreContainer, State: terminated(0)}, false},
		"failed":    {corev1.ContainerStatus{Name: restoreContainer, State: terminated(1)}, true},
		"crash-loop": {
			corev1.ContainerStatus{Name: restoreContainer, State: waiting, LastTerminationState: terminated(1)},
			true,
		},
		"other": {corev1.ContainerStatus{Name: "config-check", State: terminated(1)}, false},
	} {
		pod := &corev1.Pod{Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{tc.status}}}
		if got := initContainerFailed(pod, restoreContainer); got != tc.failed {
			t.Fatalf("%s: got %v", name, got)
		}
	}
}

// TestRestoreScriptExtractsOnce runs the init script with its fixed paths moved into a temporary
// directory. The snapshot image provides BusyBox; this exercises the same POSIX commands.
func TestRestoreScriptExtractsOnce(t *testing.T) {
	dir := t.TempDir()
	data, snapshots := filepath.Join(dir, "data"), filepath.Join(dir, "snapshot")
	for _, d := range []string{data, snapshots} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for name, content := range map[string]string{
		"node/nakamoto-neon/chainstate/index.sqlite":   "chain",
		"node/nakamoto-neon/chainstate/mempool.sqlite": "pending",
		"node/nakamoto-neon/peer.sqlite":               "peers",
		"node/nakamoto-neon/peer.sqlite-wal":           "peer log",
		"regtest/blocks/blk00000.dat":                  "blocks",
		"regtest/peers.dat":                            "bitcoin peers",
		"regtest/anchors.dat":                          "bitcoin anchors",
		"regtest/mempool.dat":                          "bitcoin pending",
		// An archive captured from a restored network carries that network's marker.
		".chainstate-restore": "source network archive checksum",
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshots, "node-01.tar.gz"), archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	script := strings.NewReplacer("/data", data, "/snapshot", snapshots).Replace(restoreScript)
	run := func(checksum string) error {
		// #nosec G204 -- Fixed shell and the package's own script; arguments are test fixtures.
		return exec.CommandContext(t.Context(), "sh", "-ec", script, "--", "node-01.tar.gz", checksum).Run()
	}
	stale := filepath.Join(data, "partial-attempt")
	if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if run(strings.Repeat("0", 64)) == nil {
		t.Fatal("a wrong checksum must fail before touching data")
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatal("a failed checksum must leave the volume untouched")
	}
	if err := run(hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	chain := filepath.Join(data, "node/nakamoto-neon/chainstate/index.sqlite")
	marker := filepath.Join(data, ".chainstate-restore")
	for path, present := range map[string]bool{
		chain: true,
		stale: false,
		filepath.Join(data, "node/nakamoto-neon/peer.sqlite"):               false,
		filepath.Join(data, "node/nakamoto-neon/peer.sqlite-wal"):           false,
		filepath.Join(data, "node/nakamoto-neon/chainstate/mempool.sqlite"): false,
		filepath.Join(data, "regtest/blocks/blk00000.dat"):                  true,
		filepath.Join(data, "regtest/peers.dat"):                            false,
		filepath.Join(data, "regtest/anchors.dat"):                          false,
		filepath.Join(data, "regtest/mempool.dat"):                          false,
		marker:          true,
		marker + ".tmp": false,
	} {
		if _, err := os.Stat(path); (err == nil) != present {
			t.Fatalf("%s present=%v after restore", path, err == nil)
		}
	}
	// #nosec G304 -- Path is inside the test's private temporary directory.
	if got, _ := os.ReadFile(marker); strings.TrimSpace(string(got)) != hex.EncodeToString(sum[:]) {
		t.Fatalf("marker must record this archive's checksum, got %q", got)
	}
	// A later start keeps the actor's own writes: the marker skips extraction.
	if err := os.WriteFile(chain, []byte("advanced"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	// #nosec G304 -- Path is inside the test's private temporary directory.
	if got, _ := os.ReadFile(chain); string(got) != "advanced" {
		t.Fatalf("restart re-extracted the snapshot: %q", got)
	}
	// An extraction interrupted right after the archive's own marker was unpacked leaves a marker
	// that does not match this archive: the next start must extract again.
	if err := os.WriteFile(marker, []byte("source network archive checksum\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	// #nosec G304 -- Path is inside the test's private temporary directory.
	if got, _ := os.ReadFile(chain); string(got) != "chain" {
		t.Fatalf("a foreign marker skipped extraction: %q", got)
	}
}
