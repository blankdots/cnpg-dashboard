package watcher

import (
	"context"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/blankdots/cnpg-dashboard/internal/store"
)

// ClusterFuncMap returns the add/update/delete handlers for Cluster informer.
func ClusterFuncMap(s store.StoreInterface) map[string]interface{} {
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
func BarmanFuncMap(s store.StoreInterface) map[string]interface{} {
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
func ObjectStoreFuncMap(s store.StoreInterface) map[string]interface{} {
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

// ScheduledBackupFuncMap returns the add/update/delete handlers for the
// CloudNativePG ScheduledBackup informer. Propagates schedule and lastScheduleTime
// onto the related BarmanItem (via the target Cluster).
func ScheduledBackupFuncMap(s store.StoreInterface) map[string]interface{} {
	parse := func(metaObj metav1.Object) (ns, clusterName, schedule, lastScheduleTime string, ok bool) {
		u, ok := metaObj.(*unstructured.Unstructured)
		if !ok {
			return "", "", "", "", false
		}
		obj := u.Object
		spec, _ := obj["spec"].(map[string]interface{})
		if spec == nil {
			return "", "", "", "", false
		}
		clusterSpec, _ := spec["cluster"].(map[string]interface{})
		clusterName, _ = clusterSpec["name"].(string)
		schedule, _ = spec["schedule"].(string)
		if clusterName == "" {
			return "", "", "", "", false
		}
		status, _ := obj["status"].(map[string]interface{})
		if status != nil {
			if t, _ := status["lastScheduleTime"].(string); t != "" {
				lastScheduleTime = t
			}
		}
		return u.GetNamespace(), clusterName, schedule, lastScheduleTime, true
	}

	return map[string]interface{}{
		"add": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			if ns, clusterName, schedule, lastScheduleTime, ok := parse(metaObj); ok {
				s.UpdateBarmanSchedule(ns, clusterName, schedule, lastScheduleTime, true)
			}
		},
		"update": func(_ context.Context, _ dynamic.Interface, _ string, _, newMeta metav1.Object) {
			if ns, clusterName, schedule, lastScheduleTime, ok := parse(newMeta); ok {
				s.UpdateBarmanSchedule(ns, clusterName, schedule, lastScheduleTime, true)
			}
		},
		"delete": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			if ns, clusterName, _, _, ok := parse(metaObj); ok {
				s.UpdateBarmanSchedule(ns, clusterName, "", "", false)
			}
		},
	}
}

// BackupFuncMap returns the add/update/delete handlers for the CloudNativePG Backup informer.
// Aggregates backup sizes per cluster and sets BarmanItem.Size (total) when the store does not provide backupsSize.
func BackupFuncMap(s store.StoreInterface) map[string]interface{} {
	parse := func(metaObj metav1.Object) (ns, clusterName, backupName string, sizeBytes int64, ok bool) {
		u, ok := metaObj.(*unstructured.Unstructured)
		if !ok {
			return "", "", "", 0, false
		}
		obj := u.Object
		spec, _ := obj["spec"].(map[string]interface{})
		if spec == nil {
			return "", "", "", 0, false
		}
		clusterSpec, _ := spec["cluster"].(map[string]interface{})
		clusterName, _ = clusterSpec["name"].(string)
		if clusterName == "" {
			return "", "", "", 0, false
		}
		ns = u.GetNamespace()
		backupName = u.GetName()
		status, _ := obj["status"].(map[string]interface{})
		if status != nil {
			// size can be string (e.g. "10Gi") or number (bytes)
			if v, ok := status["size"]; ok {
				switch val := v.(type) {
				case string:
					q, err := resource.ParseQuantity(val)
					if err == nil {
						sizeBytes = q.Value()
					}
				case float64:
					sizeBytes = int64(val)
				case int64:
					sizeBytes = val
				case int:
					sizeBytes = int64(val)
				}
			}
			if sizeBytes == 0 {
				if v, ok := status["dataSize"]; ok {
					switch n := v.(type) {
					case int64:
						sizeBytes = n
					case int:
						sizeBytes = int64(n)
					case float64:
						sizeBytes = int64(n)
					}
				}
			}
			if sizeBytes == 0 {
				if v, ok := status["backupSize"]; ok {
					if str, ok := v.(string); ok {
						q, err := resource.ParseQuantity(str)
						if err == nil {
							sizeBytes = q.Value()
						}
					}
				}
			}
		}
		return ns, clusterName, backupName, sizeBytes, true
	}

	return map[string]interface{}{
		"add": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			if ns, clusterName, backupName, sizeBytes, ok := parse(metaObj); ok {
				s.UpdateBackupSize(ns, clusterName, backupName, sizeBytes, false)
			}
		},
		"update": func(_ context.Context, _ dynamic.Interface, _ string, _, newMeta metav1.Object) {
			if ns, clusterName, backupName, sizeBytes, ok := parse(newMeta); ok {
				s.UpdateBackupSize(ns, clusterName, backupName, sizeBytes, false)
			}
		},
		"delete": func(_ context.Context, _ dynamic.Interface, _ string, metaObj metav1.Object) {
			if ns, clusterName, backupName, _, ok := parse(metaObj); ok {
				s.UpdateBackupSize(ns, clusterName, backupName, 0, true)
			}
		},
	}
}
