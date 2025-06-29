# FluxCD Trigger Operator

A Kubernetes controller that automatically triggers HelmRelease reconciliation when associated ConfigMaps are updated, enabling seamless configuration-driven GitOps workflows.

## Disclaimer

**Important:** This project is not affiliated with FluxCD or ControlPlaneIO-FluxCD. It is an independent operator designed to simplify deployments with FluxCD, specifically when using the `valuesFrom` feature in HelmRelease configurations. The FluxCD Trigger Operator provides additional functionality for triggering HelmRelease reconciliations based on ConfigMap updates, but it is not officially supported or endorsed by the FluxCD project or its maintainers.

## Overview

The FluxCD Trigger Operator monitors ConfigMaps with specific labels and annotations, automatically triggering FluxCD HelmRelease reconciliation when configuration changes are detected. This enables dynamic configuration management where ConfigMap updates can immediately trigger application redeployments without manual intervention.

## How It Works

The operator watches for ConfigMaps labeled with `github.com/uburro/fluxcd-trigger-operator: "true"` and:

1. **Monitors ConfigMap Changes**: Detects create, update, and generic events on labeled ConfigMaps
2. **Extracts HelmRelease References**: Uses annotations to identify the target HelmRelease
3. **Compares Digests**: Checks if the HelmRelease digest has changed since last reconciliation
4. **Triggers Reconciliation**: Patches the HelmRelease with force reconciliation annotations when changes are detected
5. **Autodiscovery**: Dynamically identifies HelmReleases with matching labels and annotations to include them in the reconciliation process

## Features

- **Automatic Triggering**: No manual intervention required for configuration-driven deployments
- **Autodiscovery**: Dynamically detects and includes HelmReleases based on labels and annotations
- **Digest-based Change Detection**: Prevents unnecessary reconciliations by comparing digests
- **FluxCD Integration**: Seamlessly works with existing FluxCD HelmRelease resources
- **Namespace Flexibility**: Supports cross-namespace ConfigMap to HelmRelease references
- **Configurable Concurrency**: Adjustable reconciliation performance settings

### Autodiscovery

The FluxCD Trigger Operator implements **autodiscovery** by scanning all HelmReleases in the cluster and automatically labeling those that use the `valuesFrom` feature. This ensures that HelmReleases relying on external ConfigMaps or Secrets for their values are dynamically included in the operator's reconciliation workflow.

#### How Autodiscovery Works:
1. **HelmRelease Scanning**: The operator scans all HelmReleases in the cluster.
2. **`valuesFrom` Detection**: It checks if the HelmRelease configuration includes the `valuesFrom` field, which indicates dependency on external resources like ConfigMaps or Secrets.
3. **Automatic Labeling**: For HelmReleases using `valuesFrom`, the operator adds the following labels:
   - `"uburro.github.com/fluxcd-trigger-operator": "true"`: Marks the HelmRelease as managed by the operator.
   - `"uburro.github.com/helmreleases-namespace": "<namespace>"`: Specifies the namespace of the HelmRelease.
   - `"uburro.github.com/helmreleases-name": "<name>"`: Specifies the name of the HelmRelease.

4. **Continuous Monitoring**: The operator continuously monitors the cluster for changes to HelmReleases and updates labels dynamically as needed.

## Prerequisites

- Kubernetes cluster (version 1.19+)
- FluxCD v2 with Helm Controller installed
- kubectl CLI tool
- Appropriate RBAC permissions

## Installation

### Using kubectl

```bash
kustomize build config | kubectl apply -f
```

### Verify Installation

```bash
kubectl get pods -n fluxcd-trigger-operator-system
kubectl logs -n fluxcd-trigger-operator-system deployment/fluxcd-trigger-operator
```

## Configuration

### Required ConfigMap Labels

ConfigMaps must have the following label to be monitored by the operator:

```yaml
metadata:
  labels:
    github.com/uburro/fluxcd-trigger-operator: "true"
```

### Required ConfigMap Annotations

ConfigMaps must include annotations to specify the target HelmRelease:

```yaml
metadata:
  annotations:
    # Required: Name of the target HelmRelease
    uburro.github.com/helmreleases-name: "my-app"
    
    # Optional: Namespace of the target HelmRelease (defaults to flux-system)
    uburro.github.com/helmreleases-namespace: "production"
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MAX_CONCURRENCY` | Maximum concurrent reconciliations | `10` |
| `CACHE_SYNC_TIMEOUT` | Controller cache sync timeout | `2m` |
| `LOG_LEVEL` | Logging level (debug, info, warn, error) | `info` |

## Usage

### Basic Example

1. **Create a HelmRelease** (if not already exists):

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: my-app
  namespace: flux-system
spec:
  chart:
    spec:
      chart: my-app
      sourceRef:
        kind: HelmRepository
        name: my-repo
      version: "1.0.0"
  values:
    valuesFrom:
    - kind: ConfigMap
      name: prod-env-values
      valuesKey: values.yaml
    - kind: Secret
      name: prod-tls-values
      valuesKey: crt
      targetPath: tls.crt
      optional: true
```

2. **Create a monitored ConfigMap**:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-app-config
  namespace: default
  labels:
    github.com/uburro/fluxcd-trigger-operator: "true"
  annotations:
    uburro.github.com/helmreleases-name: "my-app"
    uburro.github.com/helmreleases-namespace: "flux-system"
data:
  config.yaml: |
    database:
      host: "db.example.com"
      port: 5432
    features:
      enableAuth: true
      maxConnections: 100
```

3. **Update the ConfigMap** to trigger reconciliation:

```bash
kubectl patch configmap my-app-config -n default \
  --type='merge' -p='{"data":{"config.yaml":"database:\n  host: \"new-db.example.com\"\n  port: 5432\nfeatures:\n  enableAuth: true\n  maxConnections: 200"}}'
```

The operator will automatically detect the change and trigger HelmRelease reconciliation.

### Cross-Namespace Example

ConfigMap in `app-configs` namespace triggering HelmRelease in `production` namespace:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: production-config
  namespace: app-configs
  labels:
    github.com/uburro/fluxcd-trigger-operator: "true"
  annotations:
    uburro.github.com/helmreleases-name: "production-app"
    uburro.github.com/helmreleases-namespace: "production"
data:
  values.yaml: |
    environment=production
    replica.count=3
    resources.limits.memory=2Gi
```

## RBAC Requirements

The operator requires the following Kubernetes permissions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: fluxcd-trigger-operator
rules:
# ConfigMap permissions
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch", "patch"]

# HelmRelease permissions
- apiGroups: ["helm.toolkit.fluxcd.io"]
  resources: ["helmreleases"]
  verbs: ["get", "list", "watch", "patch"]
```

## Monitoring and Troubleshooting

### Checking Operator Status

```bash
# View operator logs
kubectl logs -n fluxcd-trigger-operator-system deployment/fluxcd-trigger-operator

# Check if ConfigMaps are being watched
kubectl get configmaps -l github.com/uburro/fluxcd-trigger-operator=true -A

# Verify HelmRelease reconciliation
kubectl get helmreleases -A
kubectl describe helmrelease my-app -n flux-system
```

### Common Issues

1. **ConfigMap changes not triggering reconciliation**
   - Verify the ConfigMap has the required label: `github.com/uburro/fluxcd-trigger-operator: "true"`
   - Check annotations for correct HelmRelease name and namespace
   - Ensure the target HelmRelease exists

2. **RBAC permission errors**
   - Verify the operator has proper ClusterRole permissions
   - Check if ServiceAccount is correctly bound to ClusterRoleBinding

3. **Cross-namespace access issues**
   - Ensure the operator has permissions to access resources in target namespaces
   - Verify namespace names in annotations are correct

### Debug Commands

```bash
# Check ConfigMap annotations and labels
kubectl get configmap my-app-config -o yaml

# View HelmRelease status and history
kubectl get helmrelease my-app -o jsonpath='{.status.history[0]}'

# Monitor operator events
kubectl get events -n fluxcd-trigger-operator-system

# Check if reconciliation was triggered
kubectl get helmrelease my-app -o jsonpath='{.metadata.annotations.reconcile\.fluxcd\.io/forceAt}'
```

## Architecture

```
┌─────────────────┐    watches    ┌─────────────────┐
│   ConfigMap     │──────────────▶│   Controller    │
│   (labeled)     │               │                 │
└─────────────────┘               └─────────────────┘
                                           │
                                           │ patches
                                           ▼
                                  ┌─────────────────┐
                                  │  HelmRelease    │
                                  │                 │
                                  └─────────────────┘
                                           │
                                           │ triggers
                                           ▼
                                  ┌─────────────────┐
                                  │ FluxCD Helm     │
                                  │ Controller      │
                                  └─────────────────┘
```

## Annotations Reference

| Annotation | Required | Description | Example |
|------------|----------|-------------|---------|
| `uburro.github.com/helmreleases-name` | Yes | Target HelmRelease name | `my-app` |
| `uburro.github.com/helmreleases-namespace` | No | Target HelmRelease namespace | `production` |
| `uburro.github.com/config-digest` | No | Last processed digest (auto-managed) | `sha256:abc123...` |

## Constants Reference

| Constant | Value | Description |
|----------|-------|-------------|
| `LabelConfigMapReconcilerNameSourceKey` | `github.com/uburro/fluxcd-trigger-operator` | Required label for monitoring |
| `AnnotationHelmReleaseNameKey` | `uburro.github.com/helmreleases-name` | HelmRelease name annotation |
| `AnnotationHelmReleaseNamespaceKey` | `uburro.github.com/helmreleases-namespace` | HelmRelease namespace annotation |
| `AnnotationDigistKey` | `uburro.github.com/config-digest` | Digest tracking annotation |
| `DefaultFluxcdNamespace` | `flux-system` | Default namespace for HelmReleases |

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Submit a pull request

## License

This project is licensed under the Apache License 2.0.
