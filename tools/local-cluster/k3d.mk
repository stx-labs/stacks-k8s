# k3d provider: k3s nodes in Docker containers.
# Cluster create/delete update the kubeconfig named by KUBECONFIG; select the repository file.
K3D := KUBECONFIG=kubeconfig k3d
CONTEXT := k3d-stacks-k8s
NODES := k3d-stacks-k8s-server-0 k3d-stacks-k8s-agent-0 k3d-stacks-k8s-agent-1
CHAOS_VALUES := ../../examples/chaos/k3s-values.yaml

define provider-create
$(K3D) cluster create --config k3d.yaml
endef

define provider-start
$(K3D) cluster start stacks-k8s && $(K3D) kubeconfig merge stacks-k8s --output kubeconfig
endef

define provider-stop
$(K3D) cluster stop stacks-k8s
endef

define provider-destroy
$(K3D) cluster delete stacks-k8s
endef
