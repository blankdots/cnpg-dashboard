package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/blankdots/cnpg-dashboard/internal/metrics"
)

// Commands wires all known action handlers onto the hub.
func Commands(h *Hub, client dynamic.Interface) {
	h.Register("trigger_backup", triggerBackup(client))
	h.Register("switchover", switchover(client))
	h.Register("refresh_cluster_metrics", refreshClusterMetrics(h, client))
}

type refreshClusterMetricsPayload struct {
	Namespace string `json:"namespace"`
	Cluster   string `json:"cluster"`
}

func refreshClusterMetrics(h *Hub, client dynamic.Interface) CommandHandler {
	return func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
		var p refreshClusterMetricsPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("invalid payload: %w", err)
		}
		if p.Namespace == "" || p.Cluster == "" {
			return nil, fmt.Errorf("namespace and cluster are required")
		}
		slog.Info("refresh_cluster_metrics", slog.String("cluster", p.Namespace+"/"+p.Cluster))
		if err := metrics.RefreshClusterMetrics(ctx, client, h.Store(), p.Namespace, p.Cluster); err != nil {
			return nil, err
		}
		slog.Info("refresh_cluster_metrics done", slog.String("cluster", p.Namespace+"/"+p.Cluster))
		return map[string]string{"status": "ok", "cluster": p.Namespace + "/" + p.Cluster}, nil
	}
}

type triggerBackupPayload struct {
	Namespace   string `json:"namespace"`
	Cluster     string `json:"cluster"`
	BackupName  string `json:"backupName,omitempty"`
}

func triggerBackup(client dynamic.Interface) CommandHandler {
	backupGVR := schema.GroupVersionResource{
		Group:    "postgresql.cnpg.io",
		Version:  "v1",
		Resource: "backups",
	}

	return func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
		var p triggerBackupPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("invalid payload: %w", err)
		}
		if p.Namespace == "" || p.Cluster == "" {
			return nil, fmt.Errorf("namespace and cluster are required")
		}

		backupName := p.BackupName
		if backupName == "" {
			backupName = fmt.Sprintf("%s-manual", p.Cluster)
		}

		obj := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "postgresql.cnpg.io/v1",
				"kind":       "Backup",
				"metadata": map[string]interface{}{
					"name":      backupName,
					"namespace": p.Namespace,
				},
				"spec": map[string]interface{}{
					"cluster": map[string]interface{}{
						"name": p.Cluster,
					},
					"method": "barmanObjectStore",
				},
			},
		}

		created, err := client.Resource(backupGVR).Namespace(p.Namespace).Create(
			ctx,
			obj,
			metav1.CreateOptions{},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create Backup: %w", err)
		}

		return map[string]string{
			"backup":    created.GetName(),
			"namespace": created.GetNamespace(),
			"status":    "created",
		}, nil
	}
}

type switchoverPayload struct {
	Namespace  string `json:"namespace"`
	Cluster    string `json:"cluster"`
	TargetNode string `json:"targetNode"`
}

func switchover(client dynamic.Interface) CommandHandler {
	clusterGVR := schema.GroupVersionResource{
		Group:    "postgresql.cnpg.io",
		Version:  "v1",
		Resource: "clusters",
	}

	return func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
		var p switchoverPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("invalid payload: %w", err)
		}
		if p.Namespace == "" || p.Cluster == "" || p.TargetNode == "" {
			return nil, fmt.Errorf("namespace, cluster, and targetNode are required")
		}

		patch := map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{
					"cnpg.io/switchoverPrimary": p.TargetNode,
				},
			},
		}
		patchBytes, err := json.Marshal(patch)
		if err != nil {
			return nil, err
		}

		_, err = client.Resource(clusterGVR).Namespace(p.Namespace).Patch(
			ctx,
			p.Cluster,
			types.MergePatchType,
			patchBytes,
			metav1.PatchOptions{},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to patch Cluster for switchover: %w", err)
		}

		return map[string]string{
			"cluster":    p.Cluster,
			"targetNode": p.TargetNode,
			"status":     "switchover initiated",
		}, nil
	}
}
