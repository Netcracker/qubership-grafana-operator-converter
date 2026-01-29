package converters

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/monitoring/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/monitoring/qubership-grafana-operator-converter/api/operator/v1beta1"
	"github.com/go-logr/logr"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/utils/ptr"
)

var reg = regexp.MustCompile(`[^A-Za-z0-9.-]`)

// createGrafanaDatasource converts GrafanaDatasource v1alpha1 to v1beta1
func (c *ConverterController) createGrafanaDatasource(datasource interface{}) {
	alphaDatasource, ok := datasource.(*v1alpha1.GrafanaDataSource)
	if !ok {
		c.log.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1alpha1 GrafanaDataSource")
		return
	}

	l := c.log.WithValues("kind", v1alpha1.GrafanaDataSourceKind, "name", alphaDatasource.Name, "ns", alphaDatasource.Namespace)

	crs, err := c.convertGrafanaDatasource(alphaDatasource)
	if err != nil {
		l.Error(err, "cannot convert some GrafanaDatasource at create")
	}
	var createdDatasource *v1beta1.GrafanaDatasource
	for _, cr := range crs {
		l.Info(fmt.Sprintf("start creating GrafanaDatasource %s/%s", cr.Namespace, cr.Name))
		createdDatasource, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaDatasources(cr.Namespace).Create(context.Background(), cr, metav1.CreateOptions{})
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				c.updateGrafanaDatasource(nil, cr)
				continue
			}
			l.Error(err, fmt.Sprintf("cannot create GrafanaDatasource %s/%s", cr.Namespace, cr.Name))
			continue
		}
		l.Info(fmt.Sprintf("GrafanaDashboard %v/%v uid:%v has been created",
			createdDatasource.GetNamespace(),
			createdDatasource.GetName(),
			createdDatasource.GetUID()))
	}
}

// updateGrafanaDatasource converts GrafanaDatasource v1alpha1 to v1beta1
func (c *ConverterController) updateGrafanaDatasource(old, new interface{}) {
	var err error
	var v1alpha1Datasource *v1alpha1.GrafanaDataSource
	var v1beta1Datasource *v1beta1.GrafanaDatasource
	var v1beta1Datasources []*v1beta1.GrafanaDatasource
	var ok bool
	var l logr.Logger
	v1alpha1Datasource, ok = new.(*v1alpha1.GrafanaDataSource)
	if ok && old != nil {
		l = c.log.WithValues("kind", v1alpha1.GrafanaDataSourceKind, "name", v1alpha1Datasource.Name, "ns", v1alpha1Datasource.Namespace)
		var v1alpha1DatasourceOld *v1alpha1.GrafanaDataSource
		v1alpha1DatasourceOld, ok = old.(*v1alpha1.GrafanaDataSource)
		if !ok {
			c.log.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1alpha1 GrafanaDataSource")
			return
		}
		if apiequality.Semantic.DeepEqual(v1alpha1DatasourceOld.Spec, v1alpha1Datasource.Spec) {
			l.Info("no diffs in GrafanaDatasource")
			return
		}
		l.Info(fmt.Sprintf("start converting GrafanaDatasource %s to %s", v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))
		v1beta1Datasources, err = c.convertGrafanaDatasource(v1alpha1Datasource)
		if err != nil {
			l.Error(err, "cannot convert some GrafanaDatasource at update")
		}
	} else {
		v1beta1Datasource, ok = new.(*v1beta1.GrafanaDatasource)
		if !ok {
			c.log.Error(fmt.Errorf("type assertion failed"), "GrafanaDatasource has an incorrect apiVersion")
			return
		}
		l = c.log.WithValues("kind", v1alpha1.GrafanaDataSourceKind, "name", v1beta1Datasource.Name, "ns", v1beta1Datasource.Namespace)
		v1beta1Datasources = append(v1beta1Datasources, v1beta1Datasource)
	}

	ctx := context.Background()
	var existingDatasource *v1beta1.GrafanaDatasource
	for _, ds := range v1beta1Datasources {
		existingDatasource, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaDatasources(ds.Namespace).Get(ctx, ds.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				var createdDatasource *v1beta1.GrafanaDatasource
				if createdDatasource, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaDatasources(ds.Namespace).Create(ctx, ds, metav1.CreateOptions{}); err == nil {
					l.Info(fmt.Sprintf("GrafanaDashboard %v/%v uid:%v has been created",
						createdDatasource.GetNamespace(),
						createdDatasource.GetName(),
						createdDatasource.GetUID()))
					continue
				}
			}
			l.Error(err, "cannot get existing GrafanaDatasource")
			continue
		}

		if apiequality.Semantic.DeepEqual(existingDatasource.Spec, ds.Spec) {
			l.Info("no updates in GrafanaDatasource")
			continue
		}

		existingDatasource.Spec = ds.Spec
		maps.Copy(existingDatasource.Annotations, ds.Annotations)
		maps.Copy(existingDatasource.Labels, ds.Labels)
		existingDatasource.OwnerReferences = ds.OwnerReferences

		var updatedDatasource *v1beta1.GrafanaDatasource
		updatedDatasource, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaDatasources(existingDatasource.Namespace).Update(ctx, existingDatasource, metav1.UpdateOptions{})
		l.Info(fmt.Sprintf("GrafanaDashboard %v/%v uid:%v has been updated",
			updatedDatasource.GetNamespace(),
			updatedDatasource.GetName(),
			updatedDatasource.GetUID()))
		if err != nil {
			l.Error(err, "cannot update GrafanaDatasource")
		}
	}
}

// convertGrafanaDatasource converts GrafanaDataSource from v1alpha1 to v1beta1
func (c *ConverterController) convertGrafanaDatasource(src *v1alpha1.GrafanaDataSource) (dst []*v1beta1.GrafanaDatasource, errs error) {
	c.log.Info(fmt.Sprintf("%s/%s conversion from %s to %s requested", src.Namespace, src.Name, v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))

	// Spec conversion
	var jsonData, secureJsonData []byte
	var err error
	dst = make([]*v1beta1.GrafanaDatasource, len(src.Spec.Datasources))
	for i, ds := range src.Spec.Datasources {
		if len(ds.CustomJsonData) != 0 {
			jsonData = ds.CustomJsonData
		} else {
			jsonData, err = json.Marshal(ds.JsonData)
			if err != nil {
				errs = errors.Join(errs, err)
				continue
			}
		}

		if len(ds.CustomSecureJsonData) != 0 {
			secureJsonData = ds.CustomSecureJsonData
		} else {
			secureJsonData, err = json.Marshal(ds.SecureJsonData)
			if err != nil {
				errs = errors.Join(errs, err)
				continue
			}
		}

		betaDatasource := &v1beta1.GrafanaDatasource{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       src.Namespace,
				Name:            fmt.Sprintf("%s-%s", src.Namespace, reg.ReplaceAllString(strings.ToLower(ds.Name), "-")),
				Labels:          src.Labels,
				Annotations:     src.Annotations,
				OwnerReferences: src.GetOwnerReferences(),
			},
		}

		uid := ds.Uid
		if strings.Contains(ds.Name, "Prometheus") {
			uid = oldDatasourceUID
		}

		betaDatasource.Spec = v1beta1.GrafanaDatasourceSpec{
			InstanceSelector:          c.ConverterConf.InstanceSelector,
			AllowCrossNamespaceImport: ptr.To(true),
			Datasource: &v1beta1.GrafanaDatasourceInternal{
				UID:            uid,
				Name:           ds.Name,
				Type:           ds.Type,
				URL:            ds.Url,
				Access:         ds.Access,
				Database:       ds.Database,
				User:           ds.User,
				OrgID:          ptr.To(int64(ds.OrgId)),
				IsDefault:      ptr.To(ds.IsDefault),
				BasicAuth:      ptr.To(ds.BasicAuth),
				BasicAuthUser:  ds.BasicAuthUser,
				Editable:       ptr.To(ds.Editable),
				JSONData:       jsonData,
				SecureJSONData: secureJsonData,
			},
			ResyncPeriod: v1beta1.DefaultResyncPeriod,
		}
		dst[i] = betaDatasource
	}

	c.log.Info(fmt.Sprintf("%s/%s has been successfully converted from %s to %s", src.Namespace, src.Name, v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))
	return dst, errs
}
