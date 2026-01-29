package converters

import (
	"context"
	"fmt"
	"maps"

	"github.com/monitoring/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/monitoring/qubership-grafana-operator-converter/api/operator/v1beta1"
	"github.com/go-logr/logr"
	"github.com/grafana/grafana-openapi-client-go/models"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/utils/ptr"
)

// createGrafanaNotificationChannel converts GrafanaNotificationChannel v1alpha1 to v1beta1
func (c *ConverterController) createGrafanaNotificationChannel(nc interface{}) {
	notificationChannel, ok := nc.(*v1alpha1.GrafanaNotificationChannel)
	if !ok {
		c.log.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1alpha1 GrafanaNotificationChannel")
		return
	}

	l := c.log.WithValues("kind", v1alpha1.GrafanaNotificationChannelKind, "name", notificationChannel.Name, "ns", notificationChannel.Namespace)

	cp, err := c.convertGrafanaNotificationChannel(notificationChannel)
	if err != nil {
		l.Error(err, "cannot convert some GrafanaNotificationChannel at create")
		return
	}

	l.Info("start creating GrafanaContactPoint")
	var createdContactPoint *v1beta1.GrafanaContactPoint
	createdContactPoint, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaContactPoints(cp.Namespace).Create(context.Background(), cp, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.updateGrafanaNotificationChannel(nil, cp)
			return
		}
		l.Error(err, "cannot create GrafanaContactPoint")
		return
	}
	l.Info(fmt.Sprintf("GrafanaContactPoint %v/%v uid:%v has been created",
		createdContactPoint.GetNamespace(),
		createdContactPoint.GetName(),
		createdContactPoint.GetUID()))
}

// updateGrafanaNotificationChannel converts GrafanaNotificationChannel v1alpha1 to v1beta1
func (c *ConverterController) updateGrafanaNotificationChannel(old, new interface{}) {
	var notificationChannel *v1alpha1.GrafanaNotificationChannel
	var contactPoint, existingContactPoint *v1beta1.GrafanaContactPoint
	var ok bool
	var l logr.Logger
	var err error
	notificationChannel, ok = new.(*v1alpha1.GrafanaNotificationChannel)
	if ok && old != nil {
		l = c.log.WithValues("kind", v1alpha1.GrafanaNotificationChannelKind, "name", notificationChannel.Name, "ns", notificationChannel.Namespace)
		var notificationChannelOld *v1alpha1.GrafanaNotificationChannel
		notificationChannelOld, ok = old.(*v1alpha1.GrafanaNotificationChannel)
		if !ok {
			c.log.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1alpha1 GrafanaNotificationChannel")
			return
		}

		if apiequality.Semantic.DeepEqual(notificationChannelOld.Spec, notificationChannel.Spec) {
			l.Info("no diffs in GrafanaNotificationChannels")
			return
		}
		l.Info(fmt.Sprintf("start converting GrafanaNotificationChannel %s to %s", v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))
		contactPoint, err = c.convertGrafanaNotificationChannel(notificationChannel)
		if err != nil {
			l.Error(err, "cannot convert some GrafanaNotificationChannel at create")
			return
		}
	} else {
		contactPoint, ok = new.(*v1beta1.GrafanaContactPoint)
		if !ok {
			l.Error(fmt.Errorf("type assertion failed"), "cannot cast to v1beta1 GrafanaContactPoint")
			return
		}
		l = c.log.WithValues("kind", v1alpha1.GrafanaNotificationChannelKind, "name", contactPoint.Name, "ns", contactPoint.Namespace)
	}

	ctx := context.Background()
	existingContactPoint, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaContactPoints(contactPoint.Namespace).Get(ctx, contactPoint.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			var createdContactPoint *v1beta1.GrafanaContactPoint
			if createdContactPoint, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaContactPoints(notificationChannel.Namespace).Create(ctx, contactPoint, metav1.CreateOptions{}); err == nil {
				l.Info(fmt.Sprintf("GrafanaContactPoint %v/%v uid:%v has been created",
					createdContactPoint.GetNamespace(),
					createdContactPoint.GetName(),
					createdContactPoint.GetUID()))
				return
			}
		}
		l.Error(err, "cannot get existing GrafanaContactPoint")
		return
	}

	if apiequality.Semantic.DeepEqual(existingContactPoint.Spec, contactPoint.Spec) {
		l.Info("no updates in GrafanaContactPoint")
		return
	}

	existingContactPoint.Spec = contactPoint.Spec
	maps.Copy(existingContactPoint.Annotations, contactPoint.GetAnnotations())
	maps.Copy(existingContactPoint.Labels, contactPoint.GetLabels())
	existingContactPoint.OwnerReferences = contactPoint.GetOwnerReferences()

	var updatedContactPoint *v1beta1.GrafanaContactPoint
	updatedContactPoint, err = c.v1beta1clientset.ObservabilityV1beta1().GrafanaContactPoints(existingContactPoint.Namespace).Update(ctx, existingContactPoint, metav1.UpdateOptions{})
	l.Info(fmt.Sprintf("GrafanaContactPoint %v/%v uid:%v has been updated",
		updatedContactPoint.GetNamespace(),
		updatedContactPoint.GetName(),
		updatedContactPoint.GetUID()))
	if err != nil {
		l.Error(err, "cannot update GrafanaContactPoint")
	}
}

// convertGrafanaNotificationChannel creates GrafanaNotificationChannel v1beta1 from GrafanaNotificationChannel v1alpha1
func (c *ConverterController) convertGrafanaNotificationChannel(src *v1alpha1.GrafanaNotificationChannel) (dst *v1beta1.GrafanaContactPoint, err error) {
	c.log.Info(fmt.Sprintf("%s/%s conversion from %s to %s requested", src.Namespace, src.Name, v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))

	var embeddedContactPoint models.EmbeddedContactPoint

	if len(src.Spec.Json) > 0 {
		err = json.Unmarshal([]byte(src.Spec.Json), &embeddedContactPoint)
		if err != nil {
			return nil, err
		}
	}

	dst = &v1beta1.GrafanaContactPoint{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       src.GetNamespace(),
			Name:            src.Name,
			Labels:          src.GetLabels(),
			Annotations:     src.GetAnnotations(),
			OwnerReferences: src.GetOwnerReferences(),
		},
		Spec: v1beta1.GrafanaContactPointSpec{
			Name:                      embeddedContactPoint.Name,
			Type:                      *embeddedContactPoint.Type,
			DisableResolveMessage:     embeddedContactPoint.DisableResolveMessage,
			Settings:                  jsonPtr(embeddedContactPoint.Settings),
			AllowCrossNamespaceImport: ptr.To(true),
			ResyncPeriod:              metav1.Duration{Duration: v1beta1.DefaultResyncPeriodDuration},
			InstanceSelector:          c.ConverterConf.InstanceSelector,
		},
	}

	c.log.Info(fmt.Sprintf("%s/%s has been successfully converted from %s to %s", src.Namespace, src.Name, v1alpha1.GroupVersion.String(), v1beta1.GroupVersion.String()))
	return dst, err
}

func jsonPtr(x interface{}) *apiextensions.JSON {
	bs, err := json.Marshal(x)
	if err != nil {
		panic(err)
	}
	ret := apiextensions.JSON{Raw: bs}
	return &ret
}
