# qubership-grafana-operator-converter

`qubership-grafana-operator-converter` is a small Kubernetes controller that need to automate migration
from Custom Resources (like `GrafanaDashboard`, `GrafanaDatasource`) from `integreatly.org/v1alpha1` to
`grafana.integreatly.org/v1beta1`.

Problem: `grafana-operator` from version v5 change API group, API version, structure of Custom Resources (CRs),
so all existing Custom Resources can't be automatically update to new version.

## Requirements

- Kubernetes 1.25+ or OpenShift 4.x+
- Helm 3.x+

## Deploy

Just execute:

```bash
helm install grafana-operator-converter charts/qubership-grafana-operator-converter
```

All parameters are described in the [chart documentation](charts/qubership-grafana-operator-converter/README.md).

## Usage

To run it converter need:

1. Deploy both set of CRs, for APIs `integreatly.org/v1alpha1` and `grafana.integreatly.org/v1beta1`
2. Deploy it in Kubernetes or OpenShift
3. Install application with old GrafanaDashboard CRs in group `integreatly.org/v1alpha1`
4. Converted CRs in new group `grafana.integreatly.org/v1beta1` will be created in the same namespace

## Resource ownership

The converter labels every generated resource with
`app.kubernetes.io/managed-by-operator=grafana-operator-converter`. It updates only resources that carry this label.

If an unmarked v1beta1 resource already has the target namespace and name, the converter reports a conflict and leaves
the resource unchanged. This prevents the converter from adopting resources managed by another product.

No converter release predates this ownership marker. If you deployed an unreleased build that created unmarked copies,
verify their ownership before adding the marker manually. Unmarked resources are otherwise treated as external.

By default, deleting a legacy dashboard does not delete its converted dashboard. Set
`grafana.converter.deleteTargetOnSourceDeletion=true` to add a Kubernetes owner reference to each converted dashboard.
Kubernetes then deletes the converted dashboard when its legacy source is deleted, even while the converter is not
running.
