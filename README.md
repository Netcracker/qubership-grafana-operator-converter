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

All parameters described in [Chart's README](charts/qubership-grafana-operator-converter/README.md).

## Usage

To run it converter need:

1. Deploy both set of CRs, for APIs `integreatly.org/v1alpha1` and `grafana.integreatly.org/v1beta1`
2. Deploy it in Kubernetes or OpenShift
3. Install application with old GrafanaDashboard CRs in group `integreatly.org/v1alpha1`
4. Converted CRs in new group `grafana.integreatly.org/v1beta1` will be created in the same namespace
