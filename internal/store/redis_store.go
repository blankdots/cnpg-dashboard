package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
)

const (
	redisKeyClusters = "cnpg:clusters"
	redisKeyBarmans  = "cnpg:barmans"
	redisChannel     = "cnpg:events"
)

// RedisStore persists clusters and barmans in Redis and uses Redis pub/sub for events.
// Local cache is the source of truth for reads; writes go to Redis and are published so other subscribers (e.g. this process's WS hub) get updates.
type RedisStore struct {
	mu          sync.RWMutex
	clusters    map[string]*ClusterItem
	barmans     map[string]*BarmanItem
	schedCache  map[string]map[string]*schedEntry
	backupSizes map[string]map[string]int64
	redis       *redis.Client
	broad       *broadcaster
}

// NewRedis creates a store backed by Redis. It loads existing data from Redis and starts a goroutine that subscribes to the events channel and forwards to the local broadcaster.
// Call Close() when done to stop the subscriber.
func NewRedis(addr string) (StoreInterface, error) {
	opt, err := redis.ParseURL(addr)
	if err != nil {
		// try as host:port
		opt = &redis.Options{Addr: addr}
	}
	client := redis.NewClient(opt)
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	r := &RedisStore{
		clusters:    make(map[string]*ClusterItem),
		barmans:     make(map[string]*BarmanItem),
		schedCache:  make(map[string]map[string]*schedEntry),
		backupSizes: make(map[string]map[string]int64),
		redis:       client,
		broad:       &broadcaster{},
	}
	if err := r.loadFromRedis(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis load: %w", err)
	}
	go r.runSubscriber(context.Background())
	return r, nil
}

func (r *RedisStore) loadFromRedis(ctx context.Context) error {
	// Load clusters
	clustersMap, err := r.redis.HGetAll(ctx, redisKeyClusters).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	for k, v := range clustersMap {
		var c ClusterItem
		if err := json.Unmarshal([]byte(v), &c); err != nil {
			continue
		}
		r.clusters[k] = &c
	}
	// Load barmans
	barmansMap, err := r.redis.HGetAll(ctx, redisKeyBarmans).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	for k, v := range barmansMap {
		var b BarmanItem
		if err := json.Unmarshal([]byte(v), &b); err != nil {
			continue
		}
		r.barmans[k] = &b
	}
	slog.Info("redis store loaded", slog.Int("clusters", len(r.clusters)), slog.Int("barmans", len(r.barmans)))
	return nil
}

func (r *RedisStore) runSubscriber(ctx context.Context) {
	pubsub := r.redis.Subscribe(ctx, redisChannel)
	defer pubsub.Close()
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var ev Event
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				slog.Warn("redis event decode", slog.Any("err", err))
				continue
			}
			r.broad.publish(ev)
		}
	}
}

func (r *RedisStore) publish(ev Event) {
	payload, err := json.Marshal(ev)
	if err != nil {
		slog.Warn("redis event encode", slog.Any("err", err))
		return
	}
	if err := r.redis.Publish(context.Background(), redisChannel, payload).Err(); err != nil {
		slog.Warn("redis publish", slog.Any("err", err))
	}
}

func (r *RedisStore) persistCluster(ctx context.Context, k string, c *ClusterItem) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return r.redis.HSet(ctx, redisKeyClusters, k, data).Err()
}

func (r *RedisStore) deleteClusterKey(ctx context.Context, k string) error {
	return r.redis.HDel(ctx, redisKeyClusters, k).Err()
}

func (r *RedisStore) persistBarman(ctx context.Context, k string, b *BarmanItem) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return r.redis.HSet(ctx, redisKeyBarmans, k, data).Err()
}

func (r *RedisStore) deleteBarmanKey(ctx context.Context, k string) error {
	return r.redis.HDel(ctx, redisKeyBarmans, k).Err()
}

func (r *RedisStore) resolveBarmanKeyForCluster(ns, clusterName string) string {
	clusterKey := key(ns, clusterName)
	cluster, ok := r.clusters[clusterKey]
	if !ok {
		return ""
	}
	if cluster.BarmanObjectName != "" {
		return key(ns, cluster.BarmanObjectName)
	}
	var single string
	for k := range r.barmans {
		if strings.HasPrefix(k, ns+"/") {
			if single != "" {
				return ""
			}
			single = k
		}
	}
	return single
}

// Clusters returns a copy of all clusters from the local cache.
func (r *RedisStore) Clusters() []ClusterItem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ClusterItem, 0, len(r.clusters))
	for _, c := range r.clusters {
		out = append(out, *c)
	}
	return out
}

// Barmans returns a copy of all barmans from the local cache.
func (r *RedisStore) Barmans() []BarmanItem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]BarmanItem, 0, len(r.barmans))
	for _, b := range r.barmans {
		out = append(out, *b)
	}
	return out
}

// Subscribe returns a channel that receives events (from Redis pub/sub, forwarded to local broadcaster).
func (r *RedisStore) Subscribe() <-chan Event {
	return r.broad.subscribe()
}

// Unsubscribe removes the channel from the broadcaster.
func (r *RedisStore) Unsubscribe(ch <-chan Event) {
	r.broad.unsubscribe(ch)
}

// AddCluster adds or updates a cluster, persists to Redis, and publishes the event.
func (r *RedisStore) AddCluster(c *ClusterItem) {
	ctx := context.Background()
	k := key(c.Namespace, c.Name)
	r.mu.Lock()
	existing, existed := r.clusters[k]
	if existed && existing != nil {
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
	r.clusters[k] = c
	for clusterName, entry := range r.schedCache[c.Namespace] {
		if clusterName == c.Name && entry != nil {
			if bKey := r.resolveBarmanKeyForCluster(c.Namespace, clusterName); bKey != "" {
				if b, ok := r.barmans[bKey]; ok {
					b.ScheduledBackup = entry.Schedule
					if entry.LastScheduleTime != "" {
						b.LastBackup = entry.LastScheduleTime
						b.LastBackupStatus = "Completed"
					}
					copy := *b
					r.mu.Unlock()
					_ = r.persistBarman(ctx, bKey, &copy)
					r.publish(Event{Type: EventUpdated, ResourceKind: ResourceBarman, Resource: &copy})
					r.mu.Lock()
				}
			}
			break
		}
	}
	if sizes, ok := r.backupSizes[key(c.Namespace, c.Name)]; ok && len(sizes) > 0 {
		if bKey := r.resolveBarmanKeyForCluster(c.Namespace, c.Name); bKey != "" {
			if b, ok := r.barmans[bKey]; ok {
				n := len(sizes)
				if n > b.TotalBackups {
					b.TotalBackups = n
				}
				if b.Size == "" || b.Size == "—" {
					var total int64
					for _, sz := range sizes {
						total += sz
					}
					if total > 0 {
						b.Size = formatBytes(total)
					} else {
						b.Size = fmt.Sprintf("%d backup(s)", b.TotalBackups)
					}
				}
				copy := *b
				r.mu.Unlock()
				_ = r.persistBarman(ctx, bKey, &copy)
				r.publish(Event{Type: EventUpdated, ResourceKind: ResourceBarman, Resource: &copy})
				r.mu.Lock()
			}
		}
	}
	r.mu.Unlock()
	if err := r.persistCluster(ctx, k, c); err != nil {
		slog.Warn("redis persist cluster", slog.String("key", k), slog.Any("err", err))
	}
	evType := EventAdded
	if existed {
		evType = EventUpdated
	}
	r.publish(Event{Type: evType, ResourceKind: ResourceCluster, Resource: c})
}

// DeleteCluster removes a cluster from the cache and Redis, and publishes the event.
func (r *RedisStore) DeleteCluster(ns, name string) {
	k := key(ns, name)
	r.mu.Lock()
	c, ok := r.clusters[k]
	delete(r.clusters, k)
	r.mu.Unlock()
	if !ok {
		return
	}
	_ = r.deleteClusterKey(context.Background(), k)
	r.publish(Event{Type: EventDeleted, ResourceKind: ResourceCluster, Resource: c})
}

// UpdateClusterNodeMetrics updates node metrics for a cluster and persists.
func (r *RedisStore) UpdateClusterNodeMetrics(ns, clusterName string, metrics map[string]NodeMetrics) bool {
	k := key(ns, clusterName)
	r.mu.Lock()
	c, ok := r.clusters[k]
	if !ok {
		r.mu.Unlock()
		return false
	}
	updated := *c
	updated.Nodes = make([]NodeInfo, len(c.Nodes))
	for i, n := range c.Nodes {
		updated.Nodes[i] = n
		if m, ok := metrics[n.Name]; ok {
			updated.Nodes[i].CpuUsage = m.CpuUsage
			updated.Nodes[i].MemUsage = m.MemUsage
		}
	}
	r.clusters[k] = &updated
	r.mu.Unlock()
	ctx := context.Background()
	if err := r.persistCluster(ctx, k, &updated); err != nil {
		slog.Warn("redis persist cluster metrics", slog.Any("err", err))
	}
	r.publish(Event{Type: EventUpdated, ResourceKind: ResourceCluster, Resource: &updated})
	return true
}

// UpdateBarmanSchedule updates schedule/lastBackup for the store backing the cluster and persists.
func (r *RedisStore) UpdateBarmanSchedule(ns, clusterName, schedule, lastScheduleTime string, set bool) {
	r.mu.Lock()
	if set {
		if r.schedCache[ns] == nil {
			r.schedCache[ns] = make(map[string]*schedEntry)
		}
		r.schedCache[ns][clusterName] = &schedEntry{Schedule: schedule, LastScheduleTime: lastScheduleTime}
	} else {
		if m := r.schedCache[ns]; m != nil {
			delete(m, clusterName)
			if len(m) == 0 {
				delete(r.schedCache, ns)
			}
		}
	}
	bKey := r.resolveBarmanKeyForCluster(ns, clusterName)
	if bKey == "" {
		r.mu.Unlock()
		return
	}
	b, ok := r.barmans[bKey]
	if !ok {
		r.mu.Unlock()
		return
	}
	if set {
		b.ScheduledBackup = schedule
		if lastScheduleTime != "" {
			b.LastBackup = lastScheduleTime
			b.LastBackupStatus = "Completed"
		}
	} else {
		b.ScheduledBackup = "—"
		b.LastBackup = "—"
		b.LastBackupStatus = "—"
	}
	copy := *b
	r.mu.Unlock()
	ctx := context.Background()
	_ = r.persistBarman(ctx, bKey, &copy)
	r.publish(Event{Type: EventUpdated, ResourceKind: ResourceBarman, Resource: &copy})
}

// UpdateBackupSize updates backup count/size for the store backing the cluster and persists.
func (r *RedisStore) UpdateBackupSize(ns, clusterName, backupName string, sizeBytes int64, isDelete bool) {
	clusterKey := key(ns, clusterName)
	r.mu.Lock()
	if r.backupSizes[clusterKey] == nil {
		r.backupSizes[clusterKey] = make(map[string]int64)
	}
	if isDelete {
		delete(r.backupSizes[clusterKey], backupName)
		if len(r.backupSizes[clusterKey]) == 0 {
			delete(r.backupSizes, clusterKey)
		}
	} else {
		r.backupSizes[clusterKey][backupName] = sizeBytes
	}
	var total int64
	for _, n := range r.backupSizes[clusterKey] {
		total += n
	}
	backupCount := len(r.backupSizes[clusterKey])
	bKey := r.resolveBarmanKeyForCluster(ns, clusterName)
	if bKey == "" {
		r.mu.Unlock()
		return
	}
	b, ok := r.barmans[bKey]
	if !ok {
		r.mu.Unlock()
		return
	}
	if backupCount > 0 {
		b.TotalBackups = backupCount
	}
	if total > 0 {
		if b.Size == "" || b.Size == "—" {
			b.Size = formatBytes(total)
		}
	} else if backupCount > 0 {
		if b.Size == "" || b.Size == "—" {
			b.Size = fmt.Sprintf("%d backup(s)", b.TotalBackups)
		}
	} else if len(r.backupSizes[clusterKey]) == 0 {
		if b.Size == "" || b.Size == "—" {
			b.Size = "—"
		}
	}
	copy := *b
	r.mu.Unlock()
	ctx := context.Background()
	_ = r.persistBarman(ctx, bKey, &copy)
	r.publish(Event{Type: EventUpdated, ResourceKind: ResourceBarman, Resource: &copy})
}

// AddBarman adds or updates a barman store, applies schedule/backup cache, persists to Redis, and publishes.
func (r *RedisStore) AddBarman(b *BarmanItem) {
	ctx := context.Background()
	k := key(b.Namespace, b.Name)
	r.mu.Lock()
	_, existed := r.barmans[k]
	r.barmans[k] = b
	for clusterName, entry := range r.schedCache[b.Namespace] {
		if entry != nil && r.resolveBarmanKeyForCluster(b.Namespace, clusterName) == k {
			b.ScheduledBackup = entry.Schedule
			if entry.LastScheduleTime != "" {
				b.LastBackup = entry.LastScheduleTime
				b.LastBackupStatus = "Completed"
			}
			break
		}
	}
	for clusterKey, sizes := range r.backupSizes {
		ns, clusterName, ok := parseClusterKey(clusterKey)
		if !ok || ns != b.Namespace || r.resolveBarmanKeyForCluster(ns, clusterName) != k {
			continue
		}
		n := len(sizes)
		if n > 0 {
			if n > b.TotalBackups {
				b.TotalBackups = n
			}
			if b.Size == "" || b.Size == "—" {
				var total int64
				for _, sz := range sizes {
					total += sz
				}
				if total > 0 {
					b.Size = formatBytes(total)
				} else {
					b.Size = fmt.Sprintf("%d backup(s)", b.TotalBackups)
				}
			}
		}
		break
	}
	if b.TotalBackups > 0 && sizeIsFallback(b.Size) {
		b.Size = fmt.Sprintf("%d backup(s)", b.TotalBackups)
	}
	r.mu.Unlock()
	if err := r.persistBarman(ctx, k, b); err != nil {
		slog.Warn("redis persist barman", slog.String("key", k), slog.Any("err", err))
	}
	evType := EventAdded
	if existed {
		evType = EventUpdated
	}
	r.publish(Event{Type: evType, ResourceKind: ResourceBarman, Resource: b})
}

// DeleteBarman removes a barman store from the cache and Redis, and publishes the event.
func (r *RedisStore) DeleteBarman(ns, name string) {
	k := key(ns, name)
	r.mu.Lock()
	b, ok := r.barmans[k]
	delete(r.barmans, k)
	r.mu.Unlock()
	if !ok {
		return
	}
	_ = r.deleteBarmanKey(context.Background(), k)
	r.publish(Event{Type: EventDeleted, ResourceKind: ResourceBarman, Resource: b})
}
