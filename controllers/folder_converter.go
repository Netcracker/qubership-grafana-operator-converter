package controllers

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1beta1"
	"github.com/go-logr/logr"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// createGrafanaFolder converts GrafanaFolder v1alpha1 to v1beta1
func (c *ConverterController) createGrafanaFolder(folder interface{}) {
	alphaFolder, ok := folder.(*v1alpha1.GrafanaFolder)
	if !ok {
		c.log.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1alpha1 GrafanaFolder")
		return
	}
	l := c.log.WithValues("kind", v1alpha1.GrafanaFolderKind, "name", alphaFolder.Name, "ns", alphaFolder.Namespace)

	cr := c.convertGrafanaFolder(alphaFolder)

	l.Info("start creating GrafanaFolder")
	betaFolder, err := c.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaFolders(cr.Namespace).Create(context.Background(), cr, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.updateGrafanaFolder(nil, cr)
			return
		}
		l.Error(err, "cannot create GrafanaFolder v1beta1 from v1alpha1")
		return
	}
	l.Info(fmt.Sprintf("GrafanaFolder %v/%v uid:%v has been created",
		betaFolder.GetNamespace(),
		betaFolder.GetName(),
		betaFolder.GetUID()))
}

// updateGrafanaFolder converts GrafanaFolder v1alpha1 to v1beta1
func (c *ConverterController) updateGrafanaFolder(old, new interface{}) {
	var folder *v1alpha1.GrafanaFolder
	var v1beta1Folder *v1beta1.GrafanaFolder
	var ok bool
	var l logr.Logger
	folder, ok = new.(*v1alpha1.GrafanaFolder)
	if ok && old != nil {
		l = c.log.WithValues("kind", v1alpha1.GrafanaFolderKind, "name", folder.Name, "ns", folder.Namespace)
		var alphaFolderOld *v1alpha1.GrafanaFolder
		alphaFolderOld, ok = old.(*v1alpha1.GrafanaFolder)
		if !ok {
			c.log.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1alpha1 GrafanaFolder")
			return
		}
		if apiequality.Semantic.DeepEqual(alphaFolderOld.Spec, folder.Spec) {
			l.Info("no diffs in GrafanaFolders")
			return
		}
		l.Info(fmt.Sprintf("start converting GrafanaFolder %s to %s", v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))
		v1beta1Folder = c.convertGrafanaFolder(folder)
	} else {
		v1beta1Folder, ok = new.(*v1beta1.GrafanaFolder)
		if !ok {
			l.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1beta1 GrafanaFolder")
			return
		}
		l = c.log.WithValues("kind", v1alpha1.GrafanaFolderKind, "name", v1beta1Folder.Name, "ns", v1beta1Folder.Namespace)
	}

	ctx := context.Background()
	existingFolder, err := c.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaFolders(v1beta1Folder.Namespace).Get(ctx, v1beta1Folder.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			var createdFolder *v1beta1.GrafanaFolder
			if createdFolder, err = c.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaFolders(v1beta1Folder.Namespace).Create(ctx, v1beta1Folder, metav1.CreateOptions{}); err == nil {
				l.Info(fmt.Sprintf("GrafanaDashboard %v/%v uid:%v has been created",
					createdFolder.GetNamespace(),
					createdFolder.GetName(),
					createdFolder.GetUID()))
				return
			}
		}
		l.Error(err, "cannot get existing GrafanaFolder")
		return
	}

	if apiequality.Semantic.DeepEqual(existingFolder.Spec, v1beta1Folder.Spec) {
		l.Info("no updates in GrafanaFolders")
		return
	}

	existingFolder.Spec = v1beta1Folder.Spec
	maps.Copy(existingFolder.Annotations, v1beta1Folder.GetAnnotations())
	maps.Copy(existingFolder.Labels, v1beta1Folder.GetLabels())
	existingFolder.OwnerReferences = v1beta1Folder.GetOwnerReferences()

	var updatedFolder *v1beta1.GrafanaFolder
	updatedFolder, err = c.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaFolders(existingFolder.Namespace).Update(ctx, existingFolder, metav1.UpdateOptions{})
	l.Info(fmt.Sprintf("GrafanaFolder %v/%v uid:%v has been updated",
		updatedFolder.GetNamespace(),
		updatedFolder.GetName(),
		updatedFolder.GetUID()))
	if err != nil {
		l.Error(err, "cannot update GrafanaFolder")
	}
}

// convertGrafanaFolder creates GrafanaFolder v1beta1 from GrafanaFolder v1alpha1
func (c *ConverterController) convertGrafanaFolder(src *v1alpha1.GrafanaFolder) (dst *v1beta1.GrafanaFolder) {
	c.log.Info(fmt.Sprintf("%s/%s conversion from %s to %s requested", src.Namespace, src.Name, v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))

	dst = &v1beta1.GrafanaFolder{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       src.GetNamespace(),
			Name:            src.Name,
			Labels:          src.GetLabels(),
			Annotations:     src.GetAnnotations(),
			OwnerReferences: src.GetOwnerReferences(),
		},
		Spec: v1beta1.GrafanaFolderSpec{
			Title:                     src.Spec.FolderName,
			Permissions:               buildFolderPermission(src.GetPermissions()),
			InstanceSelector:          c.ConverterConf.InstanceSelector,
			AllowCrossNamespaceImport: ptr.To(true),
			ResyncPeriod:              v1beta1.DefaultResyncPeriod,
		},
	}

	c.log.Info(fmt.Sprintf("%s/%s has been successfully converted from %s to %s", src.Namespace, src.Name, v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))
	return dst
}

func buildFolderPermission(folderPermissions []*v1alpha1.GrafanaPermissionItem) string {
	var b strings.Builder
	b.WriteString("{ \"items\": [ ")
	for i, item := range folderPermissions {
		if val, err := strconv.ParseInt(item.PermissionTarget, 10, 64); err == nil {
			_, _ = fmt.Fprintf(&b, "{%q: %d, \"permission\": %d}", item.PermissionTargetType, val, item.PermissionLevel)
		} else {
			_, _ = fmt.Fprintf(&b, "{%q: %q, \"permission\": %d}", item.PermissionTargetType, item.PermissionTarget, item.PermissionLevel)
		}
		if i+1 < len(folderPermissions) {
			b.WriteString(",")
		}
	}

	b.WriteString(" ]}")
	return b.String()
}
