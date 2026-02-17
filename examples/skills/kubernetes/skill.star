"""Kubernetes management skill providing kubectl tools."""

def kubectl_get(input):
    """Get Kubernetes resources by type.

    Args:
        input: A dict with key "resource" — the Kubernetes resource type
               to get (e.g. "pods", "services", "deployments").

    Returns:
        The output of kubectl get <resource>.
    """
    result = exec("kubectl", "get", input["resource"])
    return result["stdout"]

register_tool(
    name = "kubectl_get",
    description = "Get Kubernetes resources by type (pods, services, deployments, etc.)",
    handler = kubectl_get,
)
