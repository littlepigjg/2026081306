package registry

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type ServiceInstance struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Address         string    `json:"address"`
	TTLSeconds      int       `json:"ttl_seconds"`
	LastHeartbeat   time.Time `json:"last_heartbeat"`
	RegisteredAt    time.Time `json:"registered_at"`
	Status          string    `json:"status"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

func (si *ServiceInstance) IsExpired() bool {
	expireAt := si.LastHeartbeat.Add(time.Duration(si.TTLSeconds) * time.Second)
	return time.Now().After(expireAt)
}

func (si *ServiceInstance) ExpiresAt() time.Time {
	return si.LastHeartbeat.Add(time.Duration(si.TTLSeconds) * time.Second)
}

func (si *ServiceInstance) RemainingTTL() time.Duration {
	expireAt := si.ExpiresAt()
	remaining := time.Until(expireAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (si *ServiceInstance) Validate() error {
	if si.ID == "" {
		return fmt.Errorf("instance ID is required")
	}
	if si.Name == "" {
		return fmt.Errorf("instance name is required")
	}
	if si.Address == "" {
		return fmt.Errorf("instance address is required")
	}
	if si.TTLSeconds <= 0 {
		return fmt.Errorf("TTL must be positive, got %d", si.TTLSeconds)
	}
	return nil
}

type Store struct {
	mu        sync.RWMutex
	instances map[string]map[string]*ServiceInstance
	nameIndex map[string][]string
}

func NewStore() *Store {
	return &Store{
		instances: make(map[string]map[string]*ServiceInstance),
		nameIndex: make(map[string][]string),
	}
}

func (s *Store) Register(inst *ServiceInstance) error {
	if err := inst.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.instances[inst.Name] == nil {
		s.instances[inst.Name] = make(map[string]*ServiceInstance)
	}
	s.instances[inst.Name][inst.ID] = inst
	s.rebuildIndexLocked()
	return nil
}

func (s *Store) Deregister(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, instances := range s.instances {
		if _, ok := instances[id]; ok {
			delete(instances, id)
			if len(instances) == 0 {
				delete(s.instances, name)
			}
			s.rebuildIndexLocked()
			return true
		}
	}
	return false
}

func (s *Store) Heartbeat(id string) (*ServiceInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, instances := range s.instances {
		if inst, ok := instances[id]; ok {
			if inst.IsExpired() {
				return nil, fmt.Errorf("instance %s has expired", id)
			}
			inst.LastHeartbeat = time.Now()
			return inst, nil
		}
	}
	return nil, fmt.Errorf("instance %s not found", id)
}

func (s *Store) Get(name string) []*ServiceInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	instances, ok := s.instances[name]
	if !ok {
		return nil
	}

	result := make([]*ServiceInstance, 0, len(instances))
	for _, inst := range instances {
		result = append(result, inst)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].RegisteredAt.Before(result[j].RegisteredAt)
	})

	return result
}

func (s *Store) GetInstance(id string) (*ServiceInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, instances := range s.instances {
		if inst, ok := instances[id]; ok {
			return inst, nil
		}
	}
	return nil, fmt.Errorf("instance %s not found", id)
}

func (s *Store) ListAll() []*ServiceInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*ServiceInstance
	for _, instances := range s.instances {
		for _, inst := range instances {
			result = append(result, inst)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].RegisteredAt.Before(result[j].RegisteredAt)
	})

	return result
}

func (s *Store) ListNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.nameIndex))
	for name := range s.nameIndex {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, instances := range s.instances {
		count += len(instances)
	}
	return count
}

func (s *Store) CountByName(name string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	instances, ok := s.instances[name]
	if !ok {
		return 0
	}
	return len(instances)
}

func (s *Store) DeleteExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for name, instances := range s.instances {
		for id, inst := range instances {
			if inst.IsExpired() {
				delete(instances, id)
				deleted++
			}
		}
		if len(instances) == 0 {
			delete(s.instances, name)
		}
	}
	if deleted > 0 {
		s.rebuildIndexLocked()
	}
	return deleted
}

func (s *Store) rebuildIndexLocked() {
	s.nameIndex = make(map[string][]string)
	for name, instances := range s.instances {
		ids := make([]string, 0, len(instances))
		for id := range instances {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		s.nameIndex[name] = ids
	}
}

func (s *Store) GetNameIndex() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]string)
	for k, v := range s.nameIndex {
		ids := make([]string, len(v))
		copy(ids, v)
		result[k] = ids
	}
	return result
}

func (s *Store) ReplaceAll(instances []*ServiceInstance) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.instances = make(map[string]map[string]*ServiceInstance)
	for _, inst := range instances {
		if s.instances[inst.Name] == nil {
			s.instances[inst.Name] = make(map[string]*ServiceInstance)
		}
		s.instances[inst.Name][inst.ID] = inst
	}
	s.rebuildIndexLocked()
}

func (s *Store) Snapshot() []*ServiceInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*ServiceInstance, 0)
	for _, instances := range s.instances {
		for _, inst := range instances {
			if !inst.IsExpired() {
				result = append(result, inst)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].RegisteredAt.Before(result[j].RegisteredAt)
	})

	return result
}

func (s *Store) PurgeExpiredAt(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for name, instances := range s.instances {
		for id, inst := range instances {
			expireAt := inst.LastHeartbeat.Add(time.Duration(inst.TTLSeconds) * time.Second)
			if now.After(expireAt) {
				delete(instances, id)
				deleted++
			}
		}
		if len(instances) == 0 {
			delete(s.instances, name)
		}
	}
	if deleted > 0 {
		s.rebuildIndexLocked()
	}
	return deleted
}

func (s *Store) Stats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]int)
	for name, instances := range s.instances {
		stats[name] = len(instances)
	}
	return stats
}