package store

import (
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	EventAdded   = "ADDED"
	EventUpdated = "UPDATED"
	EventDeleted = "DELETED"
)

const (
	ResourceCluster   = "clusters"
	ResourceBarman    = "objectstores"
)

// Event is sent to WebSocket clients when the store changes.
type Event struct {
	Type         string      `json:"type"`
	ResourceKind string      `json:"resourceKind"`
	Resource     interface{} `json:"resource"`
}

// ClusterItem is the frontend-facing representation of a CNPG Cluster.
type ClusterItem struct {
	Name            string     `json:"name"`
	Namespace       string     `json:"namespace"`
	Status          string     `json:"status"`
	PostgresVersion string     `json:"postgresVersion"`
	Age             string     `json:"age"`
	Instances       int        `json:"instances"`
	ReadyInstances  int        `json:"readyInstances"`
	Storage         string     `json:"storage"`
	PrimaryNode     string     `json:"primaryNode"`
	BackupEnabled   bool       `json:"backupEnabled"`
	PgDataImage     string     `json:"pgDataImage,omitempty"` // status.pgDataImageInfo.image
	Nodes           []NodeInfo `json:"nodes"`
}

// NodeInfo represents a single instance in a cluster.
type NodeInfo struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	Lag       string `json:"lag"`
	CpuUsage  string `json:"cpuUsage,omitempty"`  // e.g. "125m" from metrics-server
	MemUsage  string `json:"memUsage,omitempty"`  // e.g. "256Mi" from metrics-server
}

// BarmanItem is the frontend-facing representation of a BarmanObjectStore.
type BarmanItem struct {
	Name             string `json:"name"`
	Namespace        string `json:"namespace"`
	ClusterRef       string `json:"clusterRef"`
	Cluster          string `json:"cluster"` // same as clusterRef for display
	Endpoint         string `json:"endpoint"`
	DestinationType  string `json:"destinationType"`
	RetentionPolicy  string `json:"retentionPolicy"`
	ScheduledBackup  string `json:"scheduledBackup"`
	LastBackup       string `json:"lastBackup"`
	LastBackupStatus string `json:"lastBackupStatus"`
	TotalBackups     int    `json:"totalBackups"`
	Size             string `json:"size"`
	WalEnabled       bool   `json:"walEnabled"`
	Encryption       string `json:"encryption"`
}

// Store holds clusters and barmans in memory and broadcasts events.
type Store struct {
	mu       sync.RWMutex
	clusters map[string]*ClusterItem
	barmans  map[string]*BarmanItem
	subs     []chan Event
}

// New creates an empty store.
func New() *Store {
	return &Store{
		clusters: make(map[string]*ClusterItem),
		barmans:  make(map[string]*BarmanItem),
		subs:     nil,
	}
}

func key(ns, name string) string {
	return ns + "/" + name
}

// Clusters returns a copy of all clusters.
func (s *Store) Clusters() []ClusterItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ClusterItem, 0, len(s.clusters))
	for _, c := range s.clusters {
		out = append(out, *c)
	}
	return out
}

// Barmans returns a copy of all barman object stores.
func (s *Store) Barmans() []BarmanItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]BarmanItem, 0, len(s.barmans))
	for _, b := range s.barmans {
		out = append(out, *b)
	}
	return out
}

// Subscribe returns a channel that receives store events. Call Unsubscribe when done.
func (s *Store) Subscribe() <-chan Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan Event, 64)
	s.subs = append(s.subs, ch)
	return ch
}

// Unsubscribe removes the channel from the subscriber list.
func (s *Store) Unsubscribe(ch <-chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subs {
		if sub == ch {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			close(sub)
			break
		}
	}
}

func (s *Store) broadcast(ev Event) {
	s.mu.RLock()
	subs := make([]chan Event, len(s.subs))
	copy(subs, s.subs)
	s.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// drop if full
		}
	}
}

// AddCluster adds or updates a cluster and broadcasts the event.
func (s *Store) AddCluster(c *ClusterItem) {
	k := key(c.Namespace, c.Name)
	s.mu.Lock()
	existing, existed := s.clusters[k]
	if existed && existing != nil {
		// Preserve metrics (CpuUsage/MemUsage) from existing cluster; CR does not have them
		metricsByNode := make(map[string]NodeMetrics)
		for _, n := range existing.Nodes {
			if n.CpuUsage != "" || n.MemUsage != "" {
				metricsByNode[n.Name] = NodeMetrics{CpuUsage: n.CpuUsage, MemUsage: n.MemUsage}
			}
		}
		for i := range c.Nodes {
			if m, ok := metricsByNode[c.Nodes[i].Name]; ok {
				c.Nodes[i].CpuUsage = m.CpuUsage
				c.Nodes[i].MemUsage = m.MemUsage
			}
		}
	}
	s.clusters[k] = c
	s.mu.Unlock()
	evType := EventAdded
	if existed {
		evType = EventUpdated
	}
	s.broadcast(Event{Type: evType, ResourceKind: ResourceCluster, Resource: c})
}

// DeleteCluster removes a cluster and broadcasts the event.
func (s *Store) DeleteCluster(ns, name string) {
	k := key(ns, name)
	s.mu.Lock()
	c, ok := s.clusters[k]
	delete(s.clusters, k)
	s.mu.Unlock()
	if !ok {
		return
	}
	s.broadcast(Event{Type: EventDeleted, ResourceKind: ResourceCluster, Resource: c})
}

// NodeMetrics holds CPU and memory usage strings from metrics-server.
type NodeMetrics struct {
	CpuUsage string
	MemUsage string
}

// UpdateClusterNodeMetrics updates CPU/memory usage for a cluster's nodes and broadcasts. Returns true if the cluster was found and updated.
func (s *Store) UpdateClusterNodeMetrics(ns, clusterName string, metrics map[string]NodeMetrics) bool {
	k := key(ns, clusterName)
	s.mu.Lock()
	c, ok := s.clusters[k]
	if !ok {
		s.mu.Unlock()
		return false
	}
	// Deep copy and update node metrics
	updated := *c
	updated.Nodes = make([]NodeInfo, len(c.Nodes))
	for i, n := range c.Nodes {
		updated.Nodes[i] = n
		if m, ok := metrics[n.Name]; ok {
			updated.Nodes[i].CpuUsage = m.CpuUsage
			updated.Nodes[i].MemUsage = m.MemUsage
		}
	}
	s.clusters[k] = &updated
	s.mu.Unlock()
	s.broadcast(Event{Type: EventUpdated, ResourceKind: ResourceCluster, Resource: &updated})
	return true
}

// AddBarman adds or updates a barman object store and broadcasts the event.
func (s *Store) AddBarman(b *BarmanItem) {
	k := key(b.Namespace, b.Name)
	s.mu.Lock()
	_, existed := s.barmans[k]
	s.barmans[k] = b
	s.mu.Unlock()
	evType := EventAdded
	if existed {
		evType = EventUpdated
	}
	s.broadcast(Event{Type: evType, ResourceKind: ResourceBarman, Resource: b})
}

// DeleteBarman removes a barman object store and broadcasts the event.
func (s *Store) DeleteBarman(ns, name string) {
	k := key(ns, name)
	s.mu.Lock()
	b, ok := s.barmans[k]
	delete(s.barmans, k)
	s.mu.Unlock()
	if !ok {
		return
	}
	s.broadcast(Event{Type: EventDeleted, ResourceKind: ResourceBarman, Resource: b})
}

// collectInstanceNames returns pod names from status, using instanceNames, instancesStatus, or cluster-name-N fallback.
func collectInstanceNames(status map[string]interface{}, clusterName string, instances int) []string {
	if status == nil {
		// No status yet (e.g. cluster just created); use fallback so metrics can still be fetched
		if instances <= 0 {
			instances = 1
		}
		out := make([]string, 0, instances)
		for i := 1; i <= instances; i++ {
			out = append(out, fmt.Sprintf("%s-%d", clusterName, i))
		}
		return out
	}
	// 1. instanceNames (primary source)
	if names, ok := status["instanceNames"].([]interface{}); ok {
		out := make([]string, 0, len(names))
		for _, n := range names {
			if str, ok := n.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	// 2. instancesStatus (map[PodStatus][]string) — values are pod name lists
	if m, ok := status["instancesStatus"].(map[string]interface{}); ok {
		seen := make(map[string]bool)
		for _, v := range m {
			if arr, ok := v.([]interface{}); ok {
				for _, x := range arr {
					if s, ok := x.(string); ok && s != "" && !seen[s] {
						seen[s] = true
					}
				}
			}
		}
		if len(seen) > 0 {
			out := make([]string, 0, len(seen))
			for s := range seen {
				out = append(out, s)
			}
			return out
		}
	}
	// 3. Fallback: CNPG pods are named {cluster-name}-1, {cluster-name}-2, ...
	if instances > 0 {
		out := make([]string, 0, instances)
		for i := 1; i <= instances; i++ {
			out = append(out, fmt.Sprintf("%s-%d", clusterName, i))
		}
		return out
	}
	return nil
}

// ClusterFromUnstructured converts a Cluster CR to ClusterItem.
func ClusterFromUnstructured(obj *unstructured.Unstructured) *ClusterItem {
	meta := obj.Object
	spec, _ := meta["spec"].(map[string]interface{})
	status, _ := meta["status"].(map[string]interface{})

	name := obj.GetName()
	ns := obj.GetNamespace()

	cluster := &ClusterItem{
		Name:            name,
		Namespace:       ns,
		Status:          "Unknown",
		PostgresVersion: "15",
		Age:             "—",
		Instances:       1,
		ReadyInstances:  0,
		Storage:         "—",
		PrimaryNode:     "—",
		BackupEnabled:   false,
		Nodes:           []NodeInfo{},
	}

	if v, ok := spec["instances"]; ok {
		if n, ok := toInt64(v); ok {
			cluster.Instances = int(n)
		}
	}
	if v, ok := status["phase"]; ok {
		if str, ok := v.(string); ok {
			cluster.Status = normalizeClusterPhase(str)
		}
	}
	if v, ok := status["readyInstances"]; ok {
		if n, ok := toInt64(v); ok {
			cluster.ReadyInstances = int(n)
		}
	}
	// status.pgDataImageInfo: image (e.g. ghcr.io/cloudnative-pg/postgresql:18.1-system-trixie) and majorVersion
	if info, ok := status["pgDataImageInfo"].(map[string]interface{}); ok {
		if ver, ok := toInt64(info["majorVersion"]); ok {
			cluster.PostgresVersion = formatVersion(int(ver))
		}
		if img, ok := info["image"].(string); ok && img != "" {
			cluster.PgDataImage = img
		}
	}
	if cluster.PostgresVersion == "15" {
		// Fallback to spec when pgDataImageInfo not yet populated (e.g. during creation)
		if pg, ok := spec["postgresql"].(map[string]interface{}); ok {
			if ver, ok := toInt64(pg["version"]); ok {
				cluster.PostgresVersion = formatVersion(int(ver))
			}
		}
	}
	if v, ok := spec["storage"]; ok {
		if st, ok := v.(map[string]interface{}); ok {
			if size, ok := st["size"].(string); ok {
				cluster.Storage = size
			}
		}
	}
	if v, ok := status["currentPrimary"]; ok {
		if str, ok := v.(string); ok {
			cluster.PrimaryNode = str
		}
	}
	if v, ok := status["currentPrimaryTimestamp"]; ok {
		if str, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339, str); err == nil {
				cluster.Age = formatAge(t)
			}
		}
	}
	if obj.GetCreationTimestamp() != (metav1.Time{}) {
		cluster.Age = formatAge(obj.GetCreationTimestamp().Time)
	}

	// Backup enabled if barman configuration exists
	if _, ok := spec["backup"]; ok {
		cluster.BackupEnabled = true
	}

	// Build nodes from instanceNames, instancesStatus, or cluster-name-N fallback
	nodeNames := collectInstanceNames(status, name, cluster.Instances)
	for _, podName := range nodeNames {
		cluster.Nodes = append(cluster.Nodes, NodeInfo{
			Name:   podName,
			Role:   "Standby",
			Status: "Unknown",
			Lag:    "—",
		})
	}
	if cluster.PrimaryNode != "—" {
		for i := range cluster.Nodes {
			if cluster.Nodes[i].Name == cluster.PrimaryNode {
				cluster.Nodes[i].Role = "Primary"
				cluster.Nodes[i].Status = cluster.Status
				break
			}
		}
	}

	return cluster
}

// BarmanFromUnstructured converts a BarmanObjectStore CR to BarmanItem.
func BarmanFromUnstructured(obj *unstructured.Unstructured) *BarmanItem {
	spec, _ := obj.Object["spec"].(map[string]interface{})
	status, _ := obj.Object["status"].(map[string]interface{})

	name := obj.GetName()
	ns := obj.GetNamespace()

	barman := &BarmanItem{
		Name:             name,
		Namespace:        ns,
		ClusterRef:       "",
		Cluster:          "",
		Endpoint:         "—",
		DestinationType:  "—",
		RetentionPolicy:  "—",
		ScheduledBackup:  "—",
		LastBackup:       "—",
		LastBackupStatus: "—",
		TotalBackups:     0,
		Size:             "—",
		WalEnabled:       false,
		Encryption:       "—",
	}

	if v, ok := spec["destinationPath"]; ok {
		if str, ok := v.(string); ok {
			barman.Endpoint = str
		}
	}
	if v, ok := spec["s3Credentials"]; ok {
		if _, ok := v.(map[string]interface{}); ok {
			barman.DestinationType = "S3"
		}
	}
	if v, ok := spec["azureCredentials"]; ok {
		if _, ok := v.(map[string]interface{}); ok {
			barman.DestinationType = "Azure"
		}
	}
	if v, ok := spec["googleCredentials"]; ok {
		if _, ok := v.(map[string]interface{}); ok {
			barman.DestinationType = "GCS"
		}
	}
	if v, ok := spec["data"]; ok {
		if d, ok := v.(map[string]interface{}); ok {
			if cluster, ok := d["cluster"].(string); ok {
				barman.ClusterRef = cluster
				barman.Cluster = cluster
			}
		}
	}
	if v, ok := spec["retentionPolicy"]; ok {
		if str, ok := v.(string); ok {
			barman.RetentionPolicy = str
		}
	}
	if v, ok := spec["wal"]; ok {
		if _, ok := v.(map[string]interface{}); ok {
			barman.WalEnabled = true
		}
	}
	if v, ok := status["lastBackup"]; ok {
		if str, ok := v.(string); ok {
			barman.LastBackup = str
		}
	}
	if v, ok := status["lastBackupStatus"]; ok {
		if str, ok := v.(string); ok {
			barman.LastBackupStatus = str
		}
	}
	if v, ok := status["backupsCount"]; ok {
		if n, ok := toInt64(v); ok {
			barman.TotalBackups = int(n)
		}
	}
	if v, ok := status["backupsSize"]; ok {
		if str, ok := v.(string); ok {
			barman.Size = str
		}
	}

	return barman
}

// ObjectStoreFromUnstructured converts a Barman Cloud plugin ObjectStore CR (barmancloud.cnpg.io/v1) to BarmanItem.
func ObjectStoreFromUnstructured(obj *unstructured.Unstructured) *BarmanItem {
	spec, _ := obj.Object["spec"].(map[string]interface{})
	status, _ := obj.Object["status"].(map[string]interface{})
	config, _ := spec["configuration"].(map[string]interface{})

	name := obj.GetName()
	ns := obj.GetNamespace()

	barman := &BarmanItem{
		Name:             name,
		Namespace:        ns,
		ClusterRef:       "",
		Cluster:          "",
		Endpoint:         "—",
		DestinationType:  "—",
		RetentionPolicy:  "—",
		ScheduledBackup:  "—",
		LastBackup:       "—",
		LastBackupStatus: "—",
		TotalBackups:     0,
		Size:             "—",
		WalEnabled:       false,
		Encryption:       "—",
	}

	if v, ok := spec["retentionPolicy"]; ok {
		if str, ok := v.(string); ok {
			barman.RetentionPolicy = str
		}
	}
	if v, ok := config["destinationPath"]; ok {
		if str, ok := v.(string); ok {
			barman.Endpoint = str
		}
	}
	if _, ok := config["s3Credentials"]; ok {
		barman.DestinationType = "S3"
	}
	if _, ok := config["azureCredentials"]; ok {
		barman.DestinationType = "Azure"
	}
	if _, ok := config["googleCredentials"]; ok {
		barman.DestinationType = "GCS"
	}
	if _, ok := config["wal"]; ok {
		barman.WalEnabled = true
	}
	if v, ok := status["lastBackup"]; ok {
		if str, ok := v.(string); ok {
			barman.LastBackup = str
		}
	}
	if v, ok := status["lastBackupStatus"]; ok {
		if str, ok := v.(string); ok {
			barman.LastBackupStatus = str
		}
	}
	if v, ok := status["backupsCount"]; ok {
		if n, ok := toInt64(v); ok {
			barman.TotalBackups = int(n)
		}
	}
	if v, ok := status["backupsSize"]; ok {
		if str, ok := v.(string); ok {
			barman.Size = str
		}
	}

	return barman
}

// normalizeClusterPhase maps CNPG Cluster status.phase to display-friendly statuses.
// Known phases: "Cluster in healthy state", "Cluster in degraded state", and transitional phases.
func normalizeClusterPhase(phase string) string {
	switch phase {
	case "Cluster in healthy state":
		return "Healthy"
	case "Cluster in degraded state":
		return "Degraded"
	case "Waiting for the instances to become active", "Cluster in creating state":
		return "Creating"
	default:
		if phase != "" {
			return phase
		}
		return "Unknown"
	}
}

func toInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	default:
		return 0, false
	}
}

func formatVersion(v int) string {
	return fmt.Sprintf("%d", v)
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return (d / time.Minute).String()
	}
	if d < 24*time.Hour {
		return (d / time.Hour).String()
	}
	return (d / (24 * time.Hour)).String() + "d"
}
