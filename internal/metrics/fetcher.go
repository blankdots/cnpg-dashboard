package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/blankdots/cnpg-dashboard/internal/store"
)

var metricsGVR = schema.GroupVersionResource{
	Group:    "metrics.k8s.io",
	Version:  "v1beta1",
	Resource: "pods",
}

// Run fetches pod metrics periodically and updates the store.
// It runs until ctx is cancelled. Requires metrics-server to be installed.
func Run(ctx context.Context, client dynamic.Interface, s store.StoreInterface, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Wait for informers to sync and metrics-server to be ready (aggregated API can return 503 early)
	select {
	case <-ctx.Done():
		return
	case <-time.After(15 * time.Second):
	}
	slog.Info("metrics: first fetch starting")
	fetchAndUpdate(ctx, client, s)

	for {
		select {
		case <-ctx.Done():
			slog.Info("metrics fetcher stopped")
			return
		case <-ticker.C:
			fetchAndUpdate(ctx, client, s)
		}
	}
}

func fetchAndUpdate(ctx context.Context, client dynamic.Interface, s store.StoreInterface) {
	clusters := s.Clusters()
	for _, c := range clusters {
		nodeNames := nodeNamesForCluster(c)
		if len(nodeNames) == 0 {
			slog.Debug("metrics: skipping cluster (no instance names)", slog.String("cluster", c.Namespace+"/"+c.Name))
			continue
		}
		metrics := fetchPodMetrics(ctx, client, c.Namespace, nodeNames)
		if len(metrics) == 0 {
			continue
		}
		updated := s.UpdateClusterNodeMetrics(c.Namespace, c.Name, metrics)
		if updated {
			slog.Info("metrics: updated cluster", slog.String("cluster", c.Namespace+"/"+c.Name), slog.Int("pods", len(metrics)))
		}
	}
}

// RefreshClusterMetrics fetches metrics for a single cluster and updates the store; then the store broadcasts.
// Call this when the user opens the cluster details modal so metrics are fresh.
func RefreshClusterMetrics(ctx context.Context, client dynamic.Interface, s store.StoreInterface, namespace, clusterName string) error {
	clusters := s.Clusters()
	var c *store.ClusterItem
	for i := range clusters {
		if clusters[i].Namespace == namespace && clusters[i].Name == clusterName {
			c = &clusters[i]
			break
		}
	}
	if c == nil {
		slog.Info("metrics: refresh cluster not in store", slog.String("cluster", namespace+"/"+clusterName), slog.Int("store_size", len(clusters)))
		return fmt.Errorf("cluster %s/%s not found", namespace, clusterName)
	}
	nodeNames := nodeNamesForCluster(*c)
	if len(nodeNames) == 0 {
		slog.Info("metrics: refresh no node names", slog.String("cluster", namespace+"/"+clusterName))
		return nil
	}
	metrics := fetchPodMetrics(ctx, client, namespace, nodeNames)
	if len(metrics) == 0 {
		slog.Info("metrics: refresh no metrics from API", slog.String("cluster", namespace+"/"+clusterName))
		return nil
	}
	s.UpdateClusterNodeMetrics(namespace, clusterName, metrics)
	slog.Info("metrics: refresh updated", slog.String("cluster", namespace+"/"+clusterName), slog.Int("pods", len(metrics)))
	return nil
}

// nodeNamesForCluster returns pod names to look up (from existing nodes or cluster-name-N fallback).
func nodeNamesForCluster(c store.ClusterItem) []string {
	if len(c.Nodes) > 0 {
		names := make([]string, len(c.Nodes))
		for i := range c.Nodes {
			names[i] = c.Nodes[i].Name
		}
		return names
	}
	instances := c.Instances
	if instances <= 0 {
		instances = 1
	}
	out := make([]string, 0, instances)
	for i := 1; i <= instances; i++ {
		out = append(out, fmt.Sprintf("%s-%d", c.Name, i))
	}
	return out
}

func fetchPodMetrics(ctx context.Context, client dynamic.Interface, namespace string, nodeNames []string) map[string]store.NodeMetrics {
	if len(nodeNames) == 0 {
		return nil
	}

	resource := client.Resource(metricsGVR).Namespace(namespace)
	var list *unstructured.UnstructuredList
	var err error
	for attempt, backoff := 0, 2*time.Second; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
				backoff *= 2
			}
		}
		var listErr error
		list, listErr = resource.List(ctx, metav1.ListOptions{})
		if listErr == nil {
			break
		}
		err = listErr
		// Retry on transient "unable to handle the request" (metrics-server / aggregated API not ready)
		if !isRetryableMetricsError(listErr) {
			break
		}
		slog.Debug("metrics fetch retry (server busy)", slog.String("namespace", namespace), slog.Int("attempt", attempt+1))
	}
	if err != nil {
		slog.Info("metrics fetch failed (metrics-server may not be installed)", slog.String("namespace", namespace), slog.Any("err", err))
		return nil
	}

	numItems := len(list.Items)
	if numItems == 0 {
		if raw, ok := list.Object["items"].([]interface{}); ok {
			numItems = len(raw)
		}
	}
	if numItems == 0 {
		slog.Info("metrics: PodMetrics list empty for namespace (metrics-server may not have scraped yet)", slog.String("namespace", namespace))
	}

	// Prefer typed .Items (dynamic client populates this); fallback to list.Object["items"] for raw decode
	metrics := make(map[string]store.NodeMetrics)
	if len(list.Items) > 0 {
		for i := range list.Items {
			item := &list.Items[i]
			podName := item.GetName()
			if podName == "" {
				continue
			}
			cpu, mem := extractUsage(item.Object)
			metrics[podName] = store.NodeMetrics{CpuUsage: cpu, MemUsage: mem}
		}
	} else if rawItems, ok := list.Object["items"].([]interface{}); ok {
		for _, it := range rawItems {
			u, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			meta, _ := u["metadata"].(map[string]interface{})
			podName, _ := meta["name"].(string)
			if podName == "" {
				continue
			}
			cpu, mem := extractUsage(u)
			metrics[podName] = store.NodeMetrics{CpuUsage: cpu, MemUsage: mem}
		}
	}
	if len(metrics) == 0 && (len(list.Items) > 0 || list.Object["items"] != nil) {
		slog.Debug("metrics: list had items but no usage extracted", slog.String("namespace", namespace))
	}
	return metrics
}

func extractUsage(podObj map[string]interface{}) (cpu, mem string) {
	containers, ok := podObj["containers"].([]interface{})
	if !ok || len(containers) == 0 {
		return "", ""
	}
	for _, c := range containers {
		cm, _ := c.(map[string]interface{})
		usage, _ := cm["usage"].(map[string]interface{})
		if usage == nil {
			continue
		}
		if u := quantityString(usage["cpu"]); u != "" {
			cpu = u
		}
		if u := quantityString(usage["memory"]); u != "" {
			mem = u
		}
		if cpu != "" && mem != "" {
			break
		}
	}
	return cpu, mem
}

// isRetryableMetricsError returns true for transient errors (e.g. metrics-server / aggregated API not ready).
func isRetryableMetricsError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "unable to handle the request") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "temporary failure")
}

func quantityString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%.0f", x)
	case int:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	default:
		return ""
	}
}
