package controllers

import (
	"context"
	"fmt"
	"maps"

	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1beta1"
	"github.com/go-logr/logr"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	oldDatasourceUID = "PC3E95692D54ABCC0"
	// newDatasourceUID = "$datasource"
)

// createGrafanaDashboard converts GrafanaDashboard v1alpha1 to v1beta1
func (c *ConverterController) createGrafanaDashboard(dashboard interface{}) {
	alphaDashboard, ok := dashboard.(*v1alpha1.GrafanaDashboard)
	if !ok {
		c.log.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1alpha1 GrafanaDashboard")
		return
	}
	l := c.log.WithValues("kind", v1alpha1.GrafanaDashboardKind, "name", alphaDashboard.Name, "ns", alphaDashboard.Namespace)

	cr := c.convertGrafanaDashboard(alphaDashboard)

	l.Info("start creating GrafanaDashboard")
	betaDashboard, err := c.v1beta1clientset.ObservabilityV1beta1().GrafanaDashboards(cr.Namespace).Create(context.Background(), cr, metav1.CreateOptions{})
	if err != nil {
		if apierrs.IsAlreadyExists(err) {
			c.updateGrafanaDashboard(nil, cr)
			return
		}
		l.Error(err, "cannot create GrafanaDashboard v1beta1 from v1alpha1")
		return
	}
	l.Info(fmt.Sprintf("GrafanaDashboard %v/%v uid:%v has been created",
		betaDashboard.GetNamespace(),
		betaDashboard.GetName(),
		betaDashboard.GetUID()))
}

// updateGrafanaDashboard converts GrafanaDashboard v1alpha1 to v1beta1
func (c *ConverterController) updateGrafanaDashboard(old, new interface{}) {
	var v1beta1Dashboard *v1beta1.GrafanaDashboard
	var l logr.Logger
	dashboard, ok := new.(*v1alpha1.GrafanaDashboard)
	if ok && old != nil {
		l = c.log.WithValues("kind", v1alpha1.GrafanaDashboardKind, "name", dashboard.Name, "ns", dashboard.Namespace)

		var alphaDashboardOld *v1alpha1.GrafanaDashboard
		alphaDashboardOld, ok = old.(*v1alpha1.GrafanaDashboard)
		if !ok {
			l.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1alpha1 GrafanaDashboard")
			return
		}

		if apiequality.Semantic.DeepEqual(alphaDashboardOld.Spec, dashboard.Spec) {
			l.Info("no diffs in GrafanaDashboards")
			return
		}
		l.Info(fmt.Sprintf("start converting GrafanaDashboard %s to %s", v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))
		v1beta1Dashboard = c.convertGrafanaDashboard(dashboard)
	} else {
		v1beta1Dashboard, ok = new.(*v1beta1.GrafanaDashboard)
		if !ok {
			l.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1beta1 GrafanaDashboard")
			return
		}
		l = c.log.WithValues("kind", v1alpha1.GrafanaDashboardKind, "name", v1beta1Dashboard.Name, "ns", v1beta1Dashboard.Namespace)
	}

	ctx := context.Background()
	existingDashboard, err := c.v1beta1clientset.ObservabilityV1beta1().GrafanaDashboards(v1beta1Dashboard.Namespace).Get(ctx, v1beta1Dashboard.Name, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			var createdDashboard *v1beta1.GrafanaDashboard
			if createdDashboard, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaDashboards(v1beta1Dashboard.Namespace).Create(ctx, v1beta1Dashboard, metav1.CreateOptions{}); err == nil {
				l.Info(fmt.Sprintf("GrafanaDashboard %v/%v uid:%v has been created",
					createdDashboard.GetNamespace(),
					createdDashboard.GetName(),
					createdDashboard.GetUID()))
				return
			}
		}
		l.Error(err, "cannot get existing GrafanaDashboard")
		return
	}

	if apiequality.Semantic.DeepEqual(existingDashboard.Spec, v1beta1Dashboard.Spec) {
		l.Info("no updates in GrafanaDashboards")
		return
	}

	existingDashboard.Spec = v1beta1Dashboard.Spec
	if existingDashboard.Annotations == nil {
		existingDashboard.Annotations = make(map[string]string, len(v1beta1Dashboard.Annotations))
	}
	maps.Copy(existingDashboard.Annotations, v1beta1Dashboard.Annotations)
	if existingDashboard.Labels == nil {
		existingDashboard.Labels = make(map[string]string, len(v1beta1Dashboard.Labels))
	}
	maps.Copy(existingDashboard.Labels, v1beta1Dashboard.Labels)
	existingDashboard.OwnerReferences = v1beta1Dashboard.OwnerReferences

	var updatedDashboard *v1beta1.GrafanaDashboard
	updatedDashboard, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaDashboards(existingDashboard.Namespace).Update(ctx, existingDashboard, metav1.UpdateOptions{})
	l.Info(fmt.Sprintf("GrafanaDashboard %v/%v uid:%v has been updated",
		updatedDashboard.GetNamespace(),
		updatedDashboard.GetName(),
		updatedDashboard.GetUID()))
	if err != nil {
		l.Error(err, "cannot update GrafanaDashboard")
	}
}

// convertGrafanaDashboard creates GrafanaDashboard v1beta1 from GrafanaDashboard v1alpha1
func (c *ConverterController) convertGrafanaDashboard(src *v1alpha1.GrafanaDashboard) (dst *v1beta1.GrafanaDashboard) {
	c.log.Info(fmt.Sprintf("%s/%s conversion from %s to %s requested", src.Namespace, src.Name, v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))

	dst = &v1beta1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       src.Namespace,
			Name:            src.Name,
			Labels:          src.Labels,
			Annotations:     src.Annotations,
			OwnerReferences: src.GetOwnerReferences(),
		},
	}

	// Spec conversion
	dst.Spec.Json = src.Spec.Json
	dst.Spec.GzipJson = src.Spec.GzipJson
	dst.Spec.Url = src.Spec.Url
	dst.Spec.Jsonnet = src.Spec.Jsonnet
	dst.Spec.ConfigMapRef = src.Spec.ConfigMapRef
	// src.Spec.GzipConfigMapRef
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
