# Sourced by the k3s experiment: exports network.sh placement and timeouts for EXAMPLE on
# this run's nodes. Env: EXAMPLE, NODE_PREFIX (k3s-<run id>-<attempt>).
# shellcheck shell=bash
# The minimal example keeps its earlier placement; 30-actors spreads over all agents.
if [ "$EXAMPLE" = 30-actors ]; then
  export WORKER_NODES="$NODE_PREFIX-agent-1 $NODE_PREFIX-agent-3"
  export WORKER2_NODES="$NODE_PREFIX-agent-2 $NODE_PREFIX-agent-4" RESOLVE_TIMEOUT=1200 INIT_TIMEOUT=3600
else
  export WORKER_NODES="$NODE_PREFIX-agent-1" WORKER2_NODES="$NODE_PREFIX-agent-2"
fi
export NODE_CONTROL_PLANE="$NODE_PREFIX-server"
