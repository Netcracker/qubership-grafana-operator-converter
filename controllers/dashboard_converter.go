package controllers

import (
	"fmt"

	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1beta1"
	"k8s.io/utils/ptr"
)

const (
	oldDatasourceUID = "PC3E95692D54ABCC0"
	// newDatasourceUID = "$datasource"
)

// convertGrafanaDashboard creates GrafanaDashboard v1beta1 from GrafanaDashboard v1alpha1
func (c *ConverterController) convertGrafanaDashboard(src *v1alpha1.GrafanaDashboard) (dst *v1beta1.GrafanaDashboard) {
	c.log.Info(fmt.Sprintf("%s/%s conversion from %s to %s requested", src.Namespace, src.Name, v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))

	dst = &v1beta1.GrafanaDashboard{
		ObjectMeta: convertedGrafanaDashboardMeta(src, c.ConverterConf.DeleteTargetOnSourceDeletion),
	}

	// Spec conversion
	dst.Spec.Json = src.Spec.Json
	dst.Spec.GzipJson = src.Spec.GzipJson
	dst.Spec.Url = src.Spec.Url
	dst.Spec.Jsonnet = src.Spec.Jsonnet
	dst.Spec.ConfigMapRef = src.Spec.ConfigMapRef
	// GzipConfigMapRef is not available in the target API.
	// The reconciler reads the referenced gzip data and stores it in GzipJson.
	dst.Spec.InstanceSelector = c.ConverterConf.InstanceSelector
	dst.Spec.AllowCrossNamespaceImport = ptr.To(true)
	dst.Spec.FolderTitle = src.Spec.CustomFolderName
	dst.Spec.ResyncPeriod = v1beta1.DefaultResyncPeriod

	for _, plugin := range src.Spec.Plugins {
		dst.Spec.Plugins = append(dst.Spec.Plugins, v1beta1.GrafanaPlugin{
			Name:    plugin.Name,
			Version: plugin.Version,
		})
	}
	for _, datasource := range src.Spec.Datasources {
		dst.Spec.Datasources = append(dst.Spec.Datasources, v1beta1.GrafanaDashboardDatasource{
			InputName:      datasource.InputName,
			DatasourceName: datasource.DatasourceName,
		})
	}

	if src.Spec.GrafanaCom != nil {
		dst.Spec.GrafanaCom = &v1beta1.GrafanaComDashboardReference{
			Id:       src.Spec.GrafanaCom.Id,
			Revision: src.Spec.GrafanaCom.Revision,
		}
	}

	if src.Spec.ContentCacheDuration != nil {
		dst.Spec.ContentCacheDuration = *src.Spec.ContentCacheDuration
	}

	c.log.Info(fmt.Sprintf("%s/%s has been successfully converted from %s to %s", src.Namespace, src.Name, v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))
	return dst
}
