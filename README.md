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

Install the Grafana Operator `grafana.integreatly.org/v1beta1` CRDs from the
[`qubership-monitoring-crds`](https://github.com/Netcracker/qubership-monitoring-operator/tree/main/charts/qubership-monitoring-crds)
chart first. The converter chart installs only the legacy `integreatly.org/v1alpha1` CRDs that it reads as migration
sources.

Then install the converter:

```bash
helm install grafana-operator-converter charts/qubership-grafana-operator-converter
```

All parameters are described in the [chart documentation](charts/qubership-grafana-operator-converter/README.md).

## Usage

To run it converter need:

1. Deploy both set of CRs, for APIs `integreatly.org/v1alpha1` and `grafana.integreatly.org/v1beta1`
2. Deploy it in Kubernetes or OpenShift
3. Install application with old GrafanaDashboard CRs in group `integreatly.org/v1alpha1`
4. Converted CRs in new group `grafana.integreatly.org/v1beta1` will be created in the same namespace. A converted
   dashboard is named `<legacy dashboard name>-converted`

## Resource ownership

The converter labels every generated resource with
`app.kubernetes.io/managed-by-operator=grafana-operator-converter`. It updates only resources that carry this label.

If an unmarked v1beta1 resource already has the target namespace and name, the converter reports a conflict and leaves
the resource unchanged. This prevents the converter from adopting resources managed by another product.

The `-converted` suffix keeps a converted dashboard from colliding with a v1beta1 dashboard that has the name of the
legacy dashboard. When the dashboard JSON sets `uid`, both dashboards write the same dashboard in Grafana. After you
publish a v1beta1 dashboard of your own, delete the legacy dashboard first, and then delete its `-converted` copy.
Grafana Operator can remove the dashboard from Grafana when the copy is deleted, and restores it on the next
synchronization of the remaining dashboard.

A legacy dashboard name longer than 243 characters cannot be converted, because the converted name exceeds the
Kubernetes limit of 253 characters. The converter logs the rejected write.

Builds before the suffix named the converted dashboard like the legacy dashboard. The converter does not update or
delete those copies, so each one diverges from its `-converted` replacement while both write the same dashboard in
Grafana. Delete the old copies after the upgrade. This command deletes the converted dashboards without the suffix in
one namespace:

```bash
kubectl get grafanadashboards.grafana.integreatly.org -n <namespace> -o name \
  -l app.kubernetes.io/managed-by-operator=grafana-operator-converter \
  | grep -v -- '-converted$' | xargs -r kubectl delete -n <namespace>
```

The filter keeps names that end with `-converted`. An earlier build can also have made such a copy for a legacy
dashboard that is itself named `<name>-converted`. If a legacy dashboard `<name>` exists, the converter reuses that copy
as the copy of `<name>`, so do not delete it. Delete it by name only when no legacy dashboard `<name>` exists.

A legacy dashboard with a name longer than 243 characters gets no `-converted` copy, so the command deletes its only
copy. Before you run the command, republish that dashboard under a shorter name and wait for its `-converted` copy.

When the dashboard JSON sets no `uid`, Grafana Operator uses the `metadata.uid` of the converted dashboard as the
dashboard UID in Grafana. The `-converted` replacement is a new resource, so it creates a second dashboard in Grafana,
and deleting the old copy removes the UID that existing links use. To keep that UID, do this before the cleanup:

1. Read the UID from `metadata.uid` of the old copy. Do not use `status.uid`, which changes when the converter
   reuses the copy for another legacy dashboard:

   ```bash
   kubectl get grafanadashboards.grafana.integreatly.org <name> -n <namespace> -o jsonpath='{.metadata.uid}'
   ```

2. Set that value as `uid` in the dashboard JSON of the legacy dashboard: in `spec.json`, in `spec.gzipJson`, or in the
   ConfigMap that the dashboard references.
3. Check that `status.uid` of the `<name>-converted` dashboard has the same value.

A dashboard that is loaded from `spec.url` or `spec.grafanaCom` keeps its UID only when the JSON at that source sets
`uid`.

No converter release predates this ownership marker. If you deployed an unreleased build that created unmarked copies,
verify their ownership before adding the marker manually. Unmarked resources are otherwise treated as external.

By default, deleting a legacy dashboard does not delete its converted dashboard. Set
`grafana.converter.deleteTargetOnSourceDeletion=true` to add a Kubernetes owner reference to each converted dashboard.
Kubernetes then deletes the converted dashboard when its legacy source is deleted, even while the converter is not
running.

The converter deletes a converted dashboard labeled `app.kubernetes.io/managed-by-operator=grafana-operator-converter`
when its legacy dashboard is not selected. A converted dashboard without that label stays.
[Dashboard selection](charts/qubership-grafana-operator-converter/README.md#dashboard-selection) states the selectors.
