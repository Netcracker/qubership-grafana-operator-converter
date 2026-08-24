package controllers

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"

	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1alpha1"
	"github.com/Netcracker/qubership-grafana-operator-converter/api/operator/v1beta1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/cache"
)

const gzipConfigMapReferenceIndex = "gzipConfigMapReference"

var errGzipDashboardSizeLimitExceeded = errors.New("decompressed dashboard exceeds the configured size limit")

type dashboardQueueItem struct {
	Namespace string
	Name      string
}

type permanentDashboardError struct {
	err error
}

func (e *permanentDashboardError) Error() string {
	return e.err.Error()
}

func (e *permanentDashboardError) Unwrap() error {
	return e.err
}

func newPermanentDashboardError(format string, args ...any) error {
	return &permanentDashboardError{err: fmt.Errorf(format, args...)}
}

func isPermanentDashboardError(err error) bool {
	var permanentError *permanentDashboardError
	return errors.As(err, &permanentError)
}

func (c *ConverterController) enqueueSourceDashboard(object any) {
	dashboard, ok := object.(*v1alpha1.GrafanaDashboard)
	if !ok {
		c.log.Error(fmt.Errorf("received %T", object), "Cannot enqueue source GrafanaDashboard: unexpected object type")
		return
	}
	c.dashboardQueue.Add(dashboardQueueItem{Namespace: dashboard.Namespace, Name: dashboard.Name})
}

func (c *ConverterController) enqueueTargetDashboard(object any) {
	dashboard, ok := object.(*v1beta1.GrafanaDashboard)
	if !ok {
		c.log.Error(fmt.Errorf("received %T", object), "Cannot enqueue target GrafanaDashboard: unexpected object type")
		return
	}
	if !isConverterManaged(dashboard) {
		return
	}
	c.dashboardQueue.Add(dashboardQueueItem{Namespace: dashboard.Namespace, Name: dashboard.Name})
}

func (c *ConverterController) enqueueUpdatedTargetDashboard(oldObject, newObject any) {
	oldDashboard, oldOK := oldObject.(*v1beta1.GrafanaDashboard)
	newDashboard, newOK := newObject.(*v1beta1.GrafanaDashboard)
	if !oldOK || !newOK {
		c.log.Error(fmt.Errorf("received %T and %T", oldObject, newObject), "Cannot enqueue updated target GrafanaDashboard: unexpected object type")
		return
	}
	if !isConverterManaged(oldDashboard) && !isConverterManaged(newDashboard) {
		return
	}
	if dashboardDesiredStateEqual(oldDashboard, newDashboard) {
		return
	}
	c.dashboardQueue.Add(dashboardQueueItem{Namespace: newDashboard.Namespace, Name: newDashboard.Name})
}

func (c *ConverterController) enqueueDeletedTargetDashboard(object any) {
	dashboard, ok := deletedTargetDashboard(object)
	if !ok {
		c.log.Error(fmt.Errorf("received %T", object), "Cannot enqueue deleted target GrafanaDashboard: unexpected object type")
		return
	}
	c.enqueueTargetDashboard(dashboard)
}

func deletedTargetDashboard(object any) (*v1beta1.GrafanaDashboard, bool) {
	if dashboard, ok := object.(*v1beta1.GrafanaDashboard); ok {
		return dashboard, true
	}
	tombstone, ok := object.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}
	dashboard, ok := tombstone.Obj.(*v1beta1.GrafanaDashboard)
	return dashboard, ok
}

func indexDashboardByGzipConfigMapReference(object any) ([]string, error) {
	dashboard, ok := object.(*v1alpha1.GrafanaDashboard)
	if !ok {
		return nil, fmt.Errorf("cannot index %T as a GrafanaDashboard", object)
	}
	if dashboard.Spec.GzipConfigMapRef == nil || dashboard.Spec.GzipConfigMapRef.Name == "" {
		return nil, nil
	}
	return []string{dashboard.Namespace + "/" + dashboard.Spec.GzipConfigMapRef.Name}, nil
}

func (c *ConverterController) enqueueDashboardsForConfigMap(indexer cache.Indexer, object any) {
	objectMeta, ok := objectMetadataFromEvent(object)
	if !ok {
		c.log.Error(fmt.Errorf("received %T", object), "Cannot enqueue GrafanaDashboards for ConfigMap: unexpected object type")
		return
	}
	dashboards, err := indexer.ByIndex(gzipConfigMapReferenceIndex, objectMeta.GetNamespace()+"/"+objectMeta.GetName())
	if err != nil {
		c.log.Error(err, "Cannot find GrafanaDashboards that reference ConfigMap", "name", objectMeta.GetName(), "namespace", objectMeta.GetNamespace())
		return
	}
	for _, object := range dashboards {
		c.enqueueSourceDashboard(object)
	}
}

func objectMetadataFromEvent(object any) (metav1.Object, bool) {
	if tombstone, ok := object.(cache.DeletedFinalStateUnknown); ok {
		object = tombstone.Obj
	}
	objectMeta, err := meta.Accessor(object)
	return objectMeta, err == nil
}

func (c *ConverterController) runDashboardWorker(ctx context.Context) {
	for c.processNextDashboard(ctx) {
	}
}

func (c *ConverterController) processNextDashboard(ctx context.Context) bool {
	item, shutdown := c.dashboardQueue.Get()
	if shutdown {
		return false
	}
	defer c.dashboardQueue.Done(item)

	err := c.reconcileDashboardWithTimeout(ctx, item)
	if err == nil {
		c.dashboardQueue.Forget(item)
		return true
	}

	logger := c.log.WithValues("kind", v1alpha1.GrafanaDashboardKind, "name", item.Name, "namespace", item.Namespace)
	if ctx.Err() != nil {
		c.dashboardQueue.Forget(item)
		return true
	}
	if isPermanentDashboardError(err) {
		c.dashboardQueue.Forget(item)
		logger.Error(err, "Cannot reconcile GrafanaDashboard; waiting for a new event")
		return true
	}

	logger.Error(err, "Cannot reconcile GrafanaDashboard; retrying with backoff", "retry", c.dashboardQueue.NumRequeues(item)+1)
	c.dashboardQueue.AddRateLimited(item)
	return true
}

func (c *ConverterController) reconcileDashboardWithTimeout(ctx context.Context, item dashboardQueueItem) error {
	reconcileContext, cancel := context.WithTimeout(ctx, c.apiTimeout)
	defer cancel()
	return c.reconcileDashboard(reconcileContext, item)
}

func (c *ConverterController) reconcileDashboard(ctx context.Context, item dashboardQueueItem) error {
	source, err := c.v1alpha1clientset.IntegreatlyV1alpha1().GrafanaDashboards(item.Namespace).Get(ctx, item.Name, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get source GrafanaDashboard: %w", err)
	}
	if !source.DeletionTimestamp.IsZero() {
		return nil
	}

	desired := c.convertGrafanaDashboard(source)
	if resolveErr := c.resolveGzipConfigMapReference(ctx, source, desired); resolveErr != nil {
		return resolveErr
	}
	if validationErr := validateConvertedDashboard(desired); validationErr != nil {
		return newPermanentDashboardError("converted GrafanaDashboard is invalid: %w", validationErr)
	}

	targetClient := c.v1beta1clientset.GrafanaIntegreatlyV1beta1().GrafanaDashboards(item.Namespace)
	target, err := targetClient.Get(ctx, item.Name, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("get target GrafanaDashboard: %w", err)
		}
		created, createErr := targetClient.Create(ctx, desired, metav1.CreateOptions{})
		if createErr != nil {
			return classifyDashboardWriteError("create target GrafanaDashboard", createErr)
		}
		c.log.Info("Created target GrafanaDashboard", "name", created.Name, "namespace", created.Namespace, "uid", created.UID)
		return nil
	}

	if !isConverterManaged(target) {
		return newPermanentDashboardError("target GrafanaDashboard %s/%s exists without the converter ownership marker", target.Namespace, target.Name)
	}
	if dashboardDesiredStateEqual(target, desired) {
		return nil
	}

	target.Spec = desired.Spec
	target.Annotations = maps.Clone(desired.Annotations)
	target.Labels = maps.Clone(desired.Labels)
	target.OwnerReferences = append([]metav1.OwnerReference(nil), desired.OwnerReferences...)
	updated, updateErr := targetClient.Update(ctx, target, metav1.UpdateOptions{})
	if updateErr != nil {
		return classifyDashboardWriteError("update target GrafanaDashboard", updateErr)
	}
	c.log.Info("Updated target GrafanaDashboard", "name", updated.Name, "namespace", updated.Namespace, "uid", updated.UID)
	return nil
}

func (c *ConverterController) resolveGzipConfigMapReference(ctx context.Context, source *v1alpha1.GrafanaDashboard, desired *v1beta1.GrafanaDashboard) error {
	reference := source.Spec.GzipConfigMapRef
	if reference == nil {
		return nil
	}
	if source.Spec.GzipJson != nil {
		return newPermanentDashboardError("source GrafanaDashboard cannot set both spec.gzipJson and spec.gzipConfigMapRef")
	}
	if reference.Name == "" {
		return newPermanentDashboardError("source GrafanaDashboard spec.gzipConfigMapRef.name must not be empty")
	}
	if validationErrors := validation.IsDNS1123Subdomain(reference.Name); len(validationErrors) > 0 {
		return newPermanentDashboardError(
			"source GrafanaDashboard spec.gzipConfigMapRef.name %q is invalid: %s",
			reference.Name,
			strings.Join(validationErrors, "; "),
		)
	}
	configMap, err := c.coreClientset.CoreV1().ConfigMaps(source.Namespace).Get(ctx, reference.Name, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			return newPermanentDashboardError("referenced ConfigMap %s/%s does not exist", source.Namespace, reference.Name)
		}
		return fmt.Errorf("get referenced ConfigMap %s/%s: %w", source.Namespace, reference.Name, err)
	}
	compressedDashboard, found := configMap.BinaryData[reference.Key]
	if !found {
		return newPermanentDashboardError("referenced ConfigMap %s/%s does not contain binaryData key %q", source.Namespace, reference.Name, reference.Key)
	}
	dashboardJSON, err := gunzipDashboard(compressedDashboard, c.gzipConfigMapMaxDecompressedSize)
	if err != nil {
		if errors.Is(err, errGzipDashboardSizeLimitExceeded) {
			return newPermanentDashboardError(
				"ConfigMap %s/%s binaryData key %q exceeds the %d-byte decompressed dashboard limit",
				source.Namespace,
				reference.Name,
				reference.Key,
				c.gzipConfigMapMaxDecompressedSize,
			)
		}
		return newPermanentDashboardError("ConfigMap %s/%s binaryData key %q does not contain valid gzip data: %w", source.Namespace, reference.Name, reference.Key, err)
	}
	if !json.Valid(dashboardJSON) {
		return newPermanentDashboardError("ConfigMap %s/%s binaryData key %q does not contain valid JSON", source.Namespace, reference.Name, reference.Key)
	}
	desired.Spec.GzipJson = append([]byte(nil), compressedDashboard...)
	return nil
}

func gunzipDashboard(compressed []byte, maxDecompressedSize int64) (dashboardJSON []byte, err error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := reader.Close(); err == nil {
			err = closeErr
		}
	}()

	dashboardJSON, err = io.ReadAll(io.LimitReader(reader, maxDecompressedSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(dashboardJSON)) > maxDecompressedSize {
		return nil, errGzipDashboardSizeLimitExceeded
	}
	return dashboardJSON, nil
}

func validateConvertedDashboard(dashboard *v1beta1.GrafanaDashboard) error {
	sourceTypes := dashboard.GetSourceTypes()
	if len(sourceTypes) != 1 {
		return fmt.Errorf("exactly one dashboard content source is required, found %d", len(sourceTypes))
	}
	return nil
}

func dashboardDesiredStateEqual(actual, desired *v1beta1.GrafanaDashboard) bool {
	return apiequality.Semantic.DeepEqual(actual.Spec, desired.Spec) &&
		apiequality.Semantic.DeepEqual(actual.Labels, desired.Labels) &&
		apiequality.Semantic.DeepEqual(actual.Annotations, desired.Annotations) &&
		apiequality.Semantic.DeepEqual(actual.OwnerReferences, desired.OwnerReferences)
}

func classifyDashboardWriteError(operation string, err error) error {
	if apierrs.IsInvalid(err) || apierrs.IsBadRequest(err) {
		return newPermanentDashboardError("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
