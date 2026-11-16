# kind provider: kubeadm-built nodes in Docker containers.
export KIND_EXPERIMENTAL_PROVIDER := docker
CONTEXT := kind-stacks-k8s
NODES := stacks-k8s-control-plane stacks-k8s-worker stacks-k8s-worker2
CHAOS_VALUES :=
NODE_FILTER := label=io.x-k8s.kind.cluster=stacks-k8s

define provider-create
kind create cluster --name stacks-k8s --config kind.yaml --kubeconfig kubeconfig --wait 120s
endef

define provider-start
set -eu; \
	nodes="$$(docker ps -a --filter $(NODE_FILTER) --format '{{.ID}}')"; \
	test -n "$$nodes" || { echo 'No stacks-k8s cluster exists; run make cluster-create.' >&2; exit 1; }; \
	docker start $$nodes; \
	kind export kubeconfig --name stacks-k8s --kubeconfig kubeconfig
endef

define provider-stop
set -eu; \
	nodes="$$(docker ps -a --filter $(NODE_FILTER) --format '{{.ID}}')"; \
	if test -n "$$nodes"; then docker stop --timeout 30 $$nodes; fi
endef

define provider-destroy
kind delete cluster --name stacks-k8s --kubeconfig kubeconfig
endef
