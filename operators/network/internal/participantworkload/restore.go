package participantworkload

import (
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// restoreContainer names the init container that fills a restored actor's data volume.
const restoreContainer = "restore-data"

// restoreScript extracts one snapshot archive into /data once. The marker records the archive's
// checksum and is written only after a complete, synced extraction, so an interrupted attempt is
// discarded and repeated on the next start. The marker is renamed into place and synced before
// the actor starts, so a node failure cannot keep the actor's later writes but lose the marker,
// which would discard them on the next start. A marker the archive itself carries (one captured
// from a restored network) never matches: no archive can contain its own checksum. Peer databases,
// peer anchors and mempools of both Stacks and Bitcoin Core are dropped: they hold the source
// network's peers and transactions. Archives captured before the capture tool dropped them are
// cleaned here too.
const restoreScript = `marker=/data/.chainstate-restore
[ "$(cat "$marker" 2>/dev/null)" = "$2" ] && exit 0
archive="/snapshot/$1"
[ "$(sha256sum <"$archive" | cut -d ' ' -f 1)" = "$2" ] || { echo "archive checksum mismatch" >&2; exit 1; }
find /data -mindepth 1 -delete
tar -xzf "$archive" -C /data
find /data \( -name 'peer.sqlite*' -o -name 'mempool.sqlite*' -o -name peers.dat -o -name anchors.dat \
  -o -name mempool.dat \) -delete
sync
echo "$2" >"$marker.tmp"
mv "$marker.tmp" "$marker"
sync`

// chainstateRestore selects one actor archive of a chainstate snapshot.
type chainstateRestore struct {
	image   string
	archive api.SnapshotArchive
}

// chainstateRestoreFor returns the participant's archive when it is an initial actor of a restored
// network. It reads only the frozen genesis, so the snapshot resource is not needed after freeze.
// Networks initialized from genesis, and participants added after a restore, start empty.
func chainstateRestoreFor(genesis *api.StacksGenesis, p *api.StacksNetworkParticipant) *chainstateRestore {
	restored := genesis.Spec.Source.Restored
	if restored == nil {
		return nil
	}
	for _, required := range genesis.Spec.Bootstrap.Requirements {
		if required.Participant.UID == p.UID && required.RestoredArchive != nil {
			return &chainstateRestore{image: restored.Image, archive: *required.RestoredArchive}
		}
	}
	return nil
}

// restoreInit runs before every other actor container, with the actor's own non-root identity and
// resource bounds.
func restoreInit(restore *chainstateRestore, resources corev1.ResourceRequirements) corev1.Container {
	return corev1.Container{
		Name:            restoreContainer,
		Image:           restore.image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-ec", restoreScript, "--", restore.archive.Path, restore.archive.SHA256},
		Resources:       resources,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
	}
}

// restoreImageUnavailable reports a restore image that cannot be pulled onto the actor's node.
func restoreImageUnavailable(pod *corev1.Pod) bool {
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == restoreContainer && status.State.Waiting != nil {
			switch status.State.Waiting.Reason {
			case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
				return true
			}
		}
	}
	return false
}

// initContainerFailed reports a terminated or crash-looping init container with a non-zero exit.
func initContainerFailed(pod *corev1.Pod, name string) bool {
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == name &&
			((status.State.Terminated != nil && status.State.Terminated.ExitCode != 0) ||
				(status.State.Waiting != nil &&
					status.LastTerminationState.Terminated != nil &&
					status.LastTerminationState.Terminated.ExitCode != 0)) {
			return true
		}
	}
	return false
}
