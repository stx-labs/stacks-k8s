---
name: stacks-k8s
description: Run an agent-led investigation on a disposable stacks-k8s Stacks/Bitcoin regtest network, from baseline through intervention to preserved evidence, including verifying a reported vulnerability. Use to plan and run an experiment end to end; it routes setup, topology, images, faults and telemetry to the matching stacks-k8s-* skill.
---

# Investigate with stacks-k8s

The agent coordinates the experiment. `StacksNetwork` provides steady-state network
operation; no operator plans a scenario or promises a deterministic outcome. Confirm
cluster authority and experiment scope before mutation. Use a fresh namespace and
preserve each run's inputs, UIDs, receipts, observations and findings outside tracked
product source. Keep credentials in Kubernetes Secrets or process stdin; strip
`authorization` and other credential fields before saving tool requests.

| Need | Skill |
| --- | --- |
| Cluster, operator installs, CRD upgrades, teardown order | `stacks-k8s-cluster` |
| Network manifest: participants, wiring, genesis, epochs | `stacks-k8s-topology` |
| Patched or mixed-version actor images | `stacks-k8s-images` |
| Chaos Mesh faults, Bitcoin actions, controls | `stacks-k8s-faults` |
| Telemetry, endpoints, preflight, inspect, evidence export | `stacks-k8s-observability` |

1. **Prepare.** Use a current stacks-k8s checkout and a running cluster. Build and
   load actor images before creating the network. Pick a topology that fits the claim;
   vulnerability reports often require miner and signers on separate nodes
   (`separate-roles.yaml`). Choose PoX lengths and block cadence so fault windows do
   not cover a whole prepare phase unless that is the subject.
2. **Establish identity and readiness.** Start telemetry before `Running` if
   initialization matters. Capture network, participant and Pod UIDs, runtime image
   IDs, operator rollout and root conditions. `Initialized`, `Running` and
   `Operational` are controller reports; compare native protocol progress separately.
   Run `stacks-preflight` before the first intervention; its pass does not prove fault
   admission or uninterrupted capture.
3. **Record a control.** Emit a scoped phase Event
   (`examples/experiments/phase-event.yaml`) and take spaced `stacks-inspect` captures
   through Pod-specific endpoints. A partial capture is evidence of an unavailable
   read, not an empty protocol fact. Choose external, experiment-specific outcome
   checks.
4. **Intervene.** Apply one measured intervention at a time, labelled with the network
   UID, and measure actual actor behavior before, during and after. Use
   `tools/stacks-workload` for finite, seeded load with exact TxID receipts: one nonce
   writer per funded sender, and never retry an uncertain submission automatically.
5. **Compare and preserve.** Check control, fault and recovery against fresh protocol
   facts, object/log/metric rows, recorder heartbeat and source gaps. A fault condition
   proves controller action, not packet effect; a healthy collector does not prove
   protocol health. Export a settled, bounded window with `stacks-evidence`, keep the
   manifest and workload receipts, and verify checksums after teardown.
6. **Clean up** in the order given by `stacks-k8s-cluster`. Leave the shared cluster
   running.

Distinguish reproducible inputs from best-effort dispatch and uncontrolled distributed
behavior. Report observed effects, counterexamples and uncertainty; separate designed
protocol behavior (such as a halt after a missed PoX anchor) from the effect under
test. Do not claim deterministic replay, causal minimality or a vulnerability from one
run.
