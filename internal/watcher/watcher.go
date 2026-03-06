package watcher

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/blankdots/cnpg-dashboard/internal/store"
)

// ClusterFuncMap returns the add/update/delete handlers for Cluster informer.
func ClusterFuncMap(s *store.Store) map[string]interface{} {
	return map[string]interface{}{
		"add": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			if u, ok := metaObj.(*unstructured.Unstructured); ok {
				s.AddCluster(store.ClusterFromUnstructured(u))
			}
		},
		"update": func(_ context.Context, _ dynamic.Interface, _ string, _, newMeta metav1.Object) {
			if u, ok := newMeta.(*unstructured.Unstructured); ok {
				s.AddCluster(store.ClusterFromUnstructured(u))
			}
		},
		"delete": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			s.DeleteCluster(metaObj.GetNamespace(), metaObj.GetName())
		},
	}
}

// BarmanFuncMap returns the add/update/delete handlers for BarmanObjectStore informer.
func BarmanFuncMap(s *store.Store) map[string]interface{} {
	return map[string]interface{}{
		"add": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			if u, ok := metaObj.(*unstructured.Unstructured); ok {
				s.AddBarman(store.BarmanFromUnstructured(u))
			}
		},
		"update": func(_ context.Context, _ dynamic.Interface, _ string, _, newMeta metav1.Object) {
			if u, ok := newMeta.(*unstructured.Unstructured); ok {
				s.AddBarman(store.BarmanFromUnstructured(u))
			}
		},
		"delete": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			s.DeleteBarman(metaObj.GetNamespace(), metaObj.GetName())
		},
	}
}

// ObjectStoreFuncMap returns the add/update/delete handlers for the Barman Cloud plugin's ObjectStore informer.
func ObjectStoreFuncMap(s *store.Store) map[string]interface{} {
	return map[string]interface{}{
		"add": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			if u, ok := metaObj.(*unstructured.Unstructured); ok {
				s.AddBarman(store.ObjectStoreFromUnstructured(u))
			}
		},
		"update": func(_ context.Context, _ dynamic.Interface, _ string, _, newMeta metav1.Object) {
			if u, ok := newMeta.(*unstructured.Unstructured); ok {
				s.AddBarman(store.ObjectStoreFromUnstructured(u))
			}
		},
		"delete": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			s.DeleteBarman(metaObj.GetNamespace(), metaObj.GetName())
		},
	}
}
