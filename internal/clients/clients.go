package clients

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sClient returns a dynamic Kubernetes client.
// If kubeconfig is empty it falls back to in-cluster config,
// then to ~/.kube/config for local development.
func K8sClient(kubeconfig string) (dynamic.Interface, error) {
	var (
		cfg *rest.Config
		err error
	)

	switch kubeconfig {
	case "":
		// Try in-cluster first (running inside a Pod)
		cfg, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to default kubeconfig for local dev
			cfg, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
			if err != nil {
				return nil, fmt.Errorf("cannot build k8s config (in-cluster and kubeconfig both failed): %w", err)
			}
		}
	default:
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("cannot build k8s config from %s: %w", kubeconfig, err)
		}
	}

	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot create dynamic client: %w", err)
	}
	return client, nil
}

// ResourceExists returns true if the given GVR (CRD) is installed in the cluster.
// Returns false when the CRD does not exist (e.g. BarmanObjectStore deprecated in newer CNPG).
func ResourceExists(client dynamic.Interface, gvr schema.GroupVersionResource) (bool, error) {
	_, err := client.Resource(gvr).List(context.Background(), metav1.ListOptions{Limit: 1})
	if err == nil {
		return true, nil
	}
	// CRD not installed: NotFound or "the server could not find the requested resource"
	if apierrors.IsNotFound(err) || strings.Contains(err.Error(), "could not find the requested resource") {
		return false, nil
	}
	return false, err
}

// DynamicInformer initializes and starts a dynamic informer for a CRD.
// fn must contain keys "add", "update", "delete" with the correct signatures.
// The informer runs until ctx is cancelled.
func DynamicInformer(
	ctx context.Context,
	client dynamic.Interface,
	namespace string,
	gvr schema.GroupVersionResource,
	fn map[string]interface{},
	errCh chan<- error,
) error {
	if namespace == "" {
		namespace = metav1.NamespaceAll
	}

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		client, time.Minute, namespace, nil,
	)
	informer := factory.ForResource(gvr).Informer()
	stopCh := make(chan struct{})

	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			metaObj, err := meta.Accessor(obj)
			if err == nil {
				fn["add"].(func(context.Context, dynamic.Interface, string, metav1.Object))(
					ctx, client, gvr.Resource, metaObj,
				)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldMeta, err1 := meta.Accessor(oldObj)
			newMeta, err2 := meta.Accessor(newObj)
			if err1 != nil || err2 != nil {
				return
			}
			// Only propagate if something actually changed
			if newMeta.GetResourceVersion() != oldMeta.GetResourceVersion() {
				fn["update"].(func(context.Context, dynamic.Interface, string, metav1.Object, metav1.Object))(
					ctx, client, gvr.Resource, oldMeta, newMeta,
				)
			}
		},
		DeleteFunc: func(obj interface{}) {
			metaObj, err := meta.Accessor(obj)
			if err == nil {
				fn["delete"].(func(context.Context, dynamic.Interface, string, metav1.Object))(
					ctx, client, gvr.Resource, metaObj,
				)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handlers for %s informer: %w", gvr.Resource, err)
	}

	factory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh, informer.HasSynced) {
		close(stopCh)
		errCh <- fmt.Errorf("cache sync failed for %s", gvr.Resource)
		return fmt.Errorf("cache sync failed for %s", gvr.Resource)
	}
	log.Printf("Cache synced for %s informer\n", gvr.Resource)

	<-ctx.Done()
	log.Printf("Stopping informer for %s\n", gvr.Resource)
	close(stopCh)
	return nil
}
