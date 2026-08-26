#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
chart_dir="${repository_root}/charts/qubership-grafana-operator-converter"
expected_dir="${repository_root}/tests/helm"
render_dir=$(mktemp -d)
trap 'rm -rf "${render_dir}"' EXIT

target_crd=$(find "${chart_dir}/crds" -maxdepth 1 -type f -name 'grafana.integreatly.org_*.yaml' -print -quit)
if [[ -n "${target_crd}" ]]; then
	echo "converter chart must not contain Grafana Operator v1beta1 CRDs: ${target_crd}" >&2
	exit 1
fi

legacy_crd_count=$(find "${chart_dir}/crds" -maxdepth 1 -type f -name 'integreatly.org_*.yaml' | wc -l)
[[ ${legacy_crd_count} -eq 5 ]]

helm show crds "${chart_dir}" >"${render_dir}/crds.yaml"
if grep -q '^  group: grafana\.integreatly\.org$' "${render_dir}/crds.yaml"; then
	echo "converter chart package must not render Grafana Operator v1beta1 CRDs" >&2
	exit 1
fi
[[ $(grep -c '^  group: integreatly\.org$' "${render_dir}/crds.yaml") -eq 5 ]]

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--show-only templates/rbac.yaml \
	--set grafana.converter.datasource=false \
	--set grafana.converter.folder=false \
	--set grafana.converter.notification=false \
	>"${render_dir}/dashboard-only-cluster.yaml"

diff -u \
	"${expected_dir}/dashboard-only-cluster.golden.yaml" \
	"${render_dir}/dashboard-only-cluster.yaml"

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--show-only templates/rbac.yaml \
	>"${render_dir}/all-converters-cluster.yaml"

diff -u \
	"${expected_dir}/all-converters-cluster.golden.yaml" \
	"${render_dir}/all-converters-cluster.yaml"

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--show-only templates/rbac.yaml \
	--set-string 'watchNamespaces=product-b\,product-a\,product-a' \
	--set grafana.converter.datasource=false \
	--set grafana.converter.folder=false \
	--set grafana.converter.notification=false \
	>"${render_dir}/dashboard-explicit-namespaces.yaml"

explicit_render="${render_dir}/dashboard-explicit-namespaces.yaml"
[[ $(grep -c '^kind: Role$' "${explicit_render}") -eq 2 ]]
[[ $(grep -c '^kind: RoleBinding$' "${explicit_render}") -eq 2 ]]
[[ $(grep -c '^kind: ClusterRole$' "${explicit_render}") -eq 0 ]]
[[ $(grep -c '^  namespace: monitoring$' "${explicit_render}") -eq 0 ]]
[[ $(grep -c '^  namespace: product-a$' "${explicit_render}") -eq 2 ]]
[[ $(grep -c '^  namespace: product-b$' "${explicit_render}") -eq 2 ]]
[[ $(grep -c '^    namespace: monitoring$' "${explicit_render}") -eq 2 ]]

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--show-only templates/deployment.yaml \
	--set-string 'watchNamespaces=product-b\,product-a\,product-a' \
	>"${render_dir}/deployment-explicit-namespaces.yaml"

grep -q '^              value: "product-a,product-b"$' \
	"${render_dir}/deployment-explicit-namespaces.yaml"

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--show-only templates/rbac.yaml \
	--set leaderElect=true \
	--set grafana.converter.datasource=false \
	--set grafana.converter.folder=false \
	--set grafana.converter.notification=false \
	>"${render_dir}/dashboard-leader-election.golden.yaml"

diff -u \
	"${expected_dir}/dashboard-leader-election.golden.yaml" \
	"${render_dir}/dashboard-leader-election.golden.yaml"

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--show-only templates/rbac.yaml \
	--set namespaceOverride=operators \
	--set watchNamespaces=product-a \
	--set grafana.converter.datasource=false \
	--set grafana.converter.folder=false \
	--set grafana.converter.notification=false \
	>"${render_dir}/dashboard-namespace-override.yaml"

override_render="${render_dir}/dashboard-namespace-override.yaml"
[[ $(grep -c '^kind: Role$' "${override_render}") -eq 1 ]]
[[ $(grep -c '^  namespace: monitoring$' "${override_render}") -eq 0 ]]
[[ $(grep -c '^  namespace: operators$' "${override_render}") -eq 0 ]]
[[ $(grep -c '^  namespace: product-a$' "${override_render}") -eq 2 ]]
[[ $(grep -c '^    namespace: operators$' "${override_render}") -eq 1 ]]

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--show-only templates/grafana-resources-configmap.yaml \
	--set namespaceOverride=operators \
	>"${render_dir}/config-namespace-override.yaml"

grep -q '^  namespace: operators$' "${render_dir}/config-namespace-override.yaml"

if helm template converter "${chart_dir}" \
	--namespace monitoring \
	--set-string 'watchNamespaceSelector=team:monitoring' \
	>"${render_dir}/selector-unsupported.yaml" \
	2>"${render_dir}/selector-unsupported.err"; then
	echo "watchNamespaceSelector must be rejected because converter informers do not honor it" >&2
	exit 1
fi

grep -q 'watchNamespaceSelector is not supported' "${render_dir}/selector-unsupported.err"

if helm template converter "${chart_dir}" \
	--namespace monitoring \
	--set namespaceScope=true \
	--set-string 'watchNamespaces= \, ' \
	>"${render_dir}/empty-namespace-list.yaml" \
	2>"${render_dir}/empty-namespace-list.err"; then
	echo "watchNamespaces must reject lists without a namespace" >&2
	exit 1
fi

grep -q 'watchNamespaces must contain at least one non-empty namespace' \
	"${render_dir}/empty-namespace-list.err"

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--show-only templates/rbac.yaml \
	--set namespaceScope=true \
	>"${render_dir}/release-namespace-scope.yaml"

namespace_scope_render="${render_dir}/release-namespace-scope.yaml"
[[ $(grep -c '^kind: Role$' "${namespace_scope_render}") -eq 1 ]]
[[ $(grep -c '^kind: RoleBinding$' "${namespace_scope_render}") -eq 1 ]]
[[ $(grep -c '^kind: ClusterRole$' "${namespace_scope_render}") -eq 0 ]]
[[ $(grep -c '^  namespace: monitoring$' "${namespace_scope_render}") -eq 2 ]]

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--show-only templates/deployment.yaml \
	--set namespaceScope=true \
	>"${render_dir}/deployment-release-namespace-scope.yaml"

grep -q '^              value: "monitoring"$' \
	"${render_dir}/deployment-release-namespace-scope.yaml"

if helm template converter "${chart_dir}" \
	--namespace monitoring \
	--set 'env[0].name=WATCH_NAMESPACE' \
	--set-string 'env[0].value=' \
	>"${render_dir}/reserved-env.yaml" \
	2>"${render_dir}/reserved-env.err"; then
	echo "env must not override the converter namespace scope" >&2
	exit 1
fi

grep -q 'env cannot override WATCH_NAMESPACE' "${render_dir}/reserved-env.err"

helm template converter "${chart_dir}" \
	--namespace monitoring \
	--set grafana.converter.enable=false \
	>"${render_dir}/converter-disabled.yaml"

disabled_render="${render_dir}/converter-disabled.yaml"
[[ $(grep -c '^kind: Role$' "${disabled_render}") -eq 0 ]]
[[ $(grep -c '^kind: RoleBinding$' "${disabled_render}") -eq 0 ]]
[[ $(grep -c '^kind: ClusterRole$' "${disabled_render}") -eq 0 ]]
[[ $(grep -c '^kind: ClusterRoleBinding$' "${disabled_render}") -eq 0 ]]
