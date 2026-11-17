package foundation

import (
	"cmp"
	"slices"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
)

// maxHeightCadences mirrors the API bound on height cadences.
const maxHeightCadences = 100

// CanonicalSchedule orders height cadences so equivalent schedules share one admitted value and
// fingerprint; Kubernetes map-list equality ignores order, but digests do not.
func CanonicalSchedule(s bitcoin.BitcoinBlockScheduleSpec) bitcoin.BitcoinBlockScheduleSpec {
	out := *s.DeepCopy()
	slices.SortFunc(out.Cadences, func(a, b bitcoin.HeightCadence) int {
		return cmp.Compare(a.FromHeight, b.FromHeight)
	})
	return out
}
