package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"sigs.k8s.io/yaml"
)

// stillRounds is how many consecutive refreshes of every observation must repeat the same tips.
const stillRounds = 2

const (
	// manifestFile is the ready-to-apply snapshot resource written next to the archives.
	manifestFile = "snapshot.yaml"
	// dockerfileFile builds the snapshot image from the output directory.
	dockerfileFile = "Dockerfile"
)

// report is the capture result; it holds public chain facts and checksums only.
type report struct {
	NetworkUID string                 `json:"networkUID"`
	Snapshot   string                 `json:"snapshot"`
	Manifest   string                 `json:"manifest"`
	Dockerfile string                 `json:"dockerfile"`
	Bitcoin    api.SnapshotBitcoinTip `json:"bitcoin"`
	Stacks     api.SnapshotStacksTip  `json:"stacks"`
	Archives   []archiveReport        `json:"archives"`
	Steps      []step                 `json:"steps"`
}

type archiveReport struct {
	Participant string `json:"participant"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
}

type step struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}

// capture checks the network, pauses it until its chain is still, stops it while recording the
// tips, exports every actor volume and writes the snapshot manifest. With resume, it exports a
// network an earlier capture already stopped, using the tips that capture recorded.
func capture(ctx context.Context, c *cluster, r request, progress func(string)) (report, error) {
	out := report{NetworkUID: r.NetworkUID, Snapshot: r.Snapshot}
	mark := func(name, detail string) {
		out.Steps = append(out.Steps, step{Name: name, At: time.Now().UTC()})
		progress(detail)
	}
	root, err := c.network(ctx, r.NetworkUID, r.Network)
	if err != nil {
		return out, err
	}
	// Only once the network identity is confirmed, so a wrong UID leaves nothing behind.
	if err = prepareOutput(r.Output); err != nil {
		return out, err
	}
	if r.Resume {
		tips, err := checkResumable(root)
		if err != nil {
			return out, err
		}
		if err = c.waitPhase(ctx, r.NetworkUID, r.Network, api.NetworkPhaseStopped); err != nil {
			return out, err
		}
		out.Bitcoin, out.Stacks = tips.Bitcoin, tips.Stacks
		mark("resumed", fmt.Sprintf("network %s was stopped by an earlier capture at Bitcoin %d, Stacks %d",
			r.Network, out.Bitcoin.Height, out.Stacks.Height))
	} else if err = checkCapturable(root); err != nil {
		return out, err
	}
	members, err := c.cohort(ctx, root)
	if err != nil {
		return out, err
	}
	participants, err := project(members)
	if err != nil {
		return out, err
	}
	if err = c.fundedKeysImported(ctx, root); err != nil {
		return out, err
	}
	if !r.Resume {
		if err = lateConsensus(root, members); err != nil {
			return out, err
		}
		if err = initialStake(members); err != nil {
			return out, err
		}
		mark("checked", fmt.Sprintf("network %s is Initialized; %d cohort participants", r.Network, len(members)))
		if err = stopStill(ctx, c, r, root, members, &out, mark); err != nil {
			return out, err
		}
		if err = c.imagesUnchanged(ctx, members, participants); err != nil {
			return out, err
		}
	}
	if err = exportArchives(ctx, c, r, members, participants, &out, progress); err != nil {
		return out, fmt.Errorf(
			"%w; the network is Stopped: retry the export with \"resume\": true and an empty output directory",
			err,
		)
	}
	mark("exported", fmt.Sprintf("%d archives exported", len(out.Archives)))
	return out, writeManifest(r, root, participants, &out, mark)
}

// imagesUnchanged re-reads each actor's admitted image once the network is Stopped. The manifest
// records the image read before the stop as the writer of the data; an image changed while the
// capture waited may have migrated the volume, so the capture is refused rather than published
// with a wrong writer.
func (c *cluster) imagesUnchanged(ctx context.Context, members []member, participants []api.SnapshotParticipant) error {
	recorded := map[string]string{}
	for _, p := range participants {
		if p.Archive != nil {
			recorded[p.Name] = p.Archive.Image
		}
	}
	var changed []string
	for _, m := range members {
		want, ok := recorded[m.participant.Spec.ParticipantName]
		if !ok {
			continue
		}
		var now api.StacksNetworkParticipant
		if err := c.get(ctx, participantResource, m.participant.Name, &now); err != nil {
			return err
		}
		if now.UID != m.participant.UID {
			return fatal{fmt.Errorf("participant %s identity changed", m.participant.Name)}
		}
		if got := actorImage(&now); got != want {
			changed = append(changed, fmt.Sprintf("%s (%s, now %s)", m.participant.Spec.ParticipantName, want, got))
		}
	}
	if len(changed) > 0 {
		sort.Strings(changed)
		return fatal{fmt.Errorf("actor images changed during the capture: %s; the data may have been written "+
			"by either image, so capture a network whose images stay fixed", strings.Join(changed, ", "))}
	}
	return nil
}

// stopStill optionally waits for a Bitcoin height, pauses the network until its chain is still,
// then stops it, recording the settled tips on the network for a later resume.
func stopStill(
	ctx context.Context,
	c *cluster,
	r request,
	root *api.StacksNetwork,
	members []member,
	out *report,
	mark func(name, detail string),
) error {
	var err error
	if r.BitcoinHeight > 0 {
		err = poll(ctx, fmt.Sprintf("Bitcoin height %d", r.BitcoinHeight), func() (bool, error) {
			tip, _, err := c.bitcoinView(ctx, root)
			return err == nil && tip.Height >= r.BitcoinHeight, err
		})
		if err != nil {
			return err
		}
		mark("reached-height", fmt.Sprintf("Bitcoin reached height %d", r.BitcoinHeight))
	}

	if root.Spec.Operation != api.NetworkOperationPaused {
		if err = c.patchOperation(ctx, root, api.NetworkOperationPaused, nil); err != nil {
			return err
		}
	}
	if err = c.waitPhase(ctx, r.NetworkUID, r.Network, api.NetworkPhasePaused); err != nil {
		return err
	}
	mark("paused", "network Paused; waiting for the chain to be still")

	var s settler
	err = poll(ctx, "a still chain", func() (bool, error) {
		btc, bo, err := c.bitcoinView(ctx, root)
		if err != nil {
			return false, err
		}
		stx, so, err := c.stacksView(ctx, members)
		if err != nil {
			return false, err
		}
		out.Bitcoin, out.Stacks = btc, stx
		still, why := s.step(append(bo, so...))
		if why != "" {
			return false, errors.New(why)
		}
		return still, nil
	})
	if err != nil {
		return err
	}
	mark("settled", fmt.Sprintf("chain still at Bitcoin %d, Stacks %d", out.Bitcoin.Height, out.Stacks.Height))

	recorded, err := tipsAnnotation(capturedTips{Bitcoin: out.Bitcoin, Stacks: out.Stacks})
	if err != nil {
		return err
	}
	if err = c.patchOperation(ctx, root, api.NetworkOperationStopped, recorded); err != nil {
		return err
	}
	if err = c.waitPhase(ctx, r.NetworkUID, r.Network, api.NetworkPhaseStopped); err != nil {
		return err
	}
	mark("stopped", "network Stopped; exporting actor volumes")
	return nil
}

// exportArchives exports every actor volume of the stopped network into the output directory and
// records each archive's checksum in its participant.
func exportArchives(
	ctx context.Context,
	c *cluster,
	r request,
	members []member,
	participants []api.SnapshotParticipant,
	out *report,
	progress func(string),
) error {
	// Export Pods an interrupted capture of this network left behind.
	if err := c.removeExportPods(ctx, r.NetworkUID); err != nil {
		return err
	}
	byName := map[string]*api.StacksNetworkParticipant{}
	for i := range members {
		byName[members[i].participant.Spec.ParticipantName] = &members[i].participant
	}
	for i := range participants {
		p := &participants[i]
		if p.Archive == nil {
			continue
		}
		file := filepath.Join(r.Output, p.Archive.Path)
		sum, size, err := c.export(ctx, r, byName[p.Name], file)
		if err != nil {
			return fmt.Errorf("export %s: %w", p.Name, err)
		}
		p.Archive.SHA256 = sum
		out.Archives = append(out.Archives, archiveReport{p.Name, p.Archive.Path, sum, size})
		progress(fmt.Sprintf("exported %s (%d bytes)", p.Archive.Path, size))
	}
	return nil
}

// writeManifest writes the snapshot resource and the image Dockerfile next to the archives.
func writeManifest(
	r request,
	root *api.StacksNetwork,
	participants []api.SnapshotParticipant,
	out *report,
	mark func(name, detail string),
) error {
	data, err := manifest(r.Snapshot, r.Image, root.Status.GenesisDigest, participants, out.Bitcoin, out.Stacks)
	if err != nil {
		return err
	}
	out.Manifest = filepath.Join(r.Output, manifestFile)
	if err = os.WriteFile(out.Manifest, data, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	out.Dockerfile = filepath.Join(r.Output, dockerfileFile)
	if err = os.WriteFile(out.Dockerfile, dockerfile(r.ExportImage), 0o600); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}
	mark("written", fmt.Sprintf(
		"wrote %s and %s; build the snapshot image with docker build -t %s %s, then load it into kind or push it",
		manifestFile, dockerfileFile, r.Image, r.Output,
	))
	return nil
}

// captureAnnotation records on a network the tips its capture settled on, in the same patch that
// stops it, so an export that fails afterwards can be resumed: Stopped is terminal.
const captureAnnotation = "snapshot.stacks.org/capture"

// capturedTips are the tips a capture settled on before stopping the network.
type capturedTips struct {
	Bitcoin api.SnapshotBitcoinTip `json:"bitcoin"`
	Stacks  api.SnapshotStacksTip  `json:"stacks"`
}

var hexHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

func tipsAnnotation(tips capturedTips) (map[string]string, error) {
	data, err := json.Marshal(tips)
	if err != nil {
		return nil, err
	}
	return map[string]string{captureAnnotation: string(data)}, nil
}

// checkResumable accepts only a network an earlier capture stopped, with the tips it recorded.
func checkResumable(root *api.StacksNetwork) (capturedTips, error) {
	var tips capturedTips
	switch {
	case root.Spec.Operation != api.NetworkOperationStopped:
		return tips, errors.New("resume requires a network an earlier capture stopped; this one is not Stopped")
	case root.Status.GenesisRef == nil || root.Status.GenesisDigest == "":
		return tips, errors.New("network has no frozen genesis")
	}
	recorded, ok := root.Annotations[captureAnnotation]
	if !ok {
		return tips, errors.New("network was not stopped by a capture: no recorded tips to resume from")
	}
	if err := json.Unmarshal([]byte(recorded), &tips); err != nil ||
		!hexHash.MatchString(tips.Bitcoin.BlockHash) || !hexHash.MatchString(tips.Stacks.BlockHash) ||
		!hexHash.MatchString(tips.Stacks.IndexBlockID) || tips.Bitcoin.Height <= 0 {
		return capturedTips{}, errors.New("recorded capture tips are invalid")
	}
	return tips, nil
}

// dockerfile builds the snapshot image: the archives under /snapshot on a BusyBox-compatible base
// that provides the restore script's tools. The archives are written owner-only and COPY keeps
// file modes, so --chmod opens them to the restore user. BuildKit applies it to the /snapshot
// directory it creates as well, which needs the execute bit to be entered: 0555, not 0444.
func dockerfile(base string) []byte {
	return []byte("# Chainstate snapshot image, built by: docker build -t <image> <this directory>\n" +
		"FROM " + base + "\n" +
		"COPY --chmod=0555 *.tar.gz /snapshot/\n")
}

// settler decides when the chain is still: every observation refreshed stillRounds times in a row
// with the same tips, and all Bitcoin nodes and all Stacks nodes agreeing. It compares operator
// timestamps only with each other, never with the local clock.
type settler struct {
	last   map[string]time.Time
	view   string
	rounds int
}

// step consumes one read of every observation. It returns whether the chain is still, or why
// the read cannot count.
func (s *settler) step(seen []observation) (bool, string) {
	tips := map[string]string{}
	for _, o := range seen {
		domain := strings.SplitN(o.source, "/", 2)[0]
		if t, ok := tips[domain]; ok && t != o.tip {
			s.last, s.rounds = nil, 0
			return false, domain + " nodes disagree on the tip"
		}
		tips[domain] = o.tip
	}
	if s.last != nil {
		for _, o := range seen {
			if !o.observedAt.After(s.last[o.source]) {
				return false, ""
			}
		}
	}
	parts := make([]string, 0, len(tips))
	for domain, tip := range tips {
		parts = append(parts, domain+"="+tip)
	}
	sort.Strings(parts)
	view := strings.Join(parts, ",")
	if s.last != nil && view == s.view {
		s.rounds++
	} else {
		s.rounds = 0
	}
	s.view = view
	s.last = map[string]time.Time{}
	for _, o := range seen {
		s.last[o.source] = o.observedAt
	}
	return s.rounds >= stillRounds, ""
}

// snapshotManifest is the applied form of the snapshot, without server-owned fields.
type snapshotManifest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec api.StacksChainstateSnapshotSpec `json:"spec"`
}

// manifest renders a namespace-free StacksChainstateSnapshot to apply wherever it is restored.
func manifest(
	name, image, genesisDigest string,
	participants []api.SnapshotParticipant,
	btc api.SnapshotBitcoinTip,
	stx api.SnapshotStacksTip,
) ([]byte, error) {
	m := snapshotManifest{APIVersion: api.GroupVersion.String(), Kind: api.KindStacksChainstateSnapshot}
	m.Metadata.Name = name
	m.Spec = api.StacksChainstateSnapshotSpec{
		Image:         image,
		GenesisDigest: genesisDigest,
		Participants:  participants,
		Bitcoin:       btc,
		Stacks:        stx,
	}
	return yaml.Marshal(m)
}
