"""Kubernetes management skill providing kubectl tools."""

def kubectl_get(resource):
    """Get Kubernetes resources by type.

    Args:
        resource: The Kubernetes resource type to get (e.g. "pods", "services", "deployments").

    Returns:
        The output of kubectl get <resource>.
    """
    result = shell_exec("kubectl", ["get", resource])
    return result

register_tool(
    name = "kubectl_get",
    description = "Get Kubernetes resources by type (pods, services, deployments, etc.)",
    fn = kubectl_get,
)
