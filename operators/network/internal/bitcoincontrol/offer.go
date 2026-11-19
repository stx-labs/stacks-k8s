package bitcoincontrol

import (
	"context"
	"encoding/json"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// assignOffer replaces the execution's offer only if it still holds the offer that was read.
// Conditioning on that field rather than resourceVersion keeps frequent worker status writes
// from starving assignment.
func assignOffer(
	ctx context.Context,
	c client.Client,
	target *bitcoin.BitcoinExecution,
	offer *bitcoin.BitcoinBlockOffer,
) error {
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": target.UID},
		{"op": "test", "path": "/spec/offer", "value": target.Spec.Offer},
		{"op": "add", "path": "/spec/offer", "value": offer},
	})
	if err != nil {
		return err
	}
	return c.Patch(ctx, target, client.RawPatch(types.JSONPatchType, patch))
}
