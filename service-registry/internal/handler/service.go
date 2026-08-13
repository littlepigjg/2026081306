package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"service-registry/internal/registry"
)

func (h *Handler) GetServiceByName(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := fmt.Sprintf("/api/%s/service/", h.cfg.APIVersion)
	name := strings.TrimPrefix(path, prefix)

	if name == "" {
		h.writeError(w, http.StatusBadRequest, "service name is required")
		return
	}

	h.log.Debug("Querying service: name=%s", name)

	instances := h.store.Get(name)

	if len(instances) == 0 {
		h.writeJSON(w, http.StatusOK, []ServiceInstanceResponse{})
		return
	}

	resp := make([]ServiceInstanceResponse, 0, len(instances))
	for _, inst := range instances {
		resp = append(resp, ServiceInstanceResponse{
			ID:            inst.ID,
			Address:       inst.Address,
			LastHeartbeat: inst.LastHeartbeat,
			Name:          inst.Name,
			Status:        inst.Status,
		})
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetServiceByNameFixed(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := fmt.Sprintf("/api/%s/service/", h.cfg.APIVersion)
	name := strings.TrimPrefix(path, prefix)

	if name == "" {
		h.writeError(w, http.StatusBadRequest, "service name is required")
		return
	}

	h.log.Debug("Querying service: name=%s", name)

	instances := h.store.Get(name)

	if len(instances) == 0 {
		h.writeJSON(w, http.StatusOK, []ServiceInstanceResponse{})
		return
	}

	resp := []ServiceInstanceResponse{}
	for _, inst := range instances {
		resp = append(resp, ServiceInstanceResponse{
			ID:            inst.ID,
			Address:       inst.Address,
			LastHeartbeat: inst.LastHeartbeat,
			Name:          inst.Name,
			Status:        inst.Status,
		})
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetServiceByNameWithDetail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := fmt.Sprintf("/api/%s/service/", h.cfg.APIVersion)
	name := strings.TrimPrefix(path, prefix)

	if name == "" {
		h.writeError(w, http.StatusBadRequest, "service name is required")
		return
	}

	instances := h.store.Get(name)
	if len(instances) == 0 {
		h.writeJSON(w, http.StatusOK, []map[string]interface{}{})
		return
	}

	resp := buildServiceResponse(instances, h.cfg.MinTTL)
	h.writeJSON(w, http.StatusOK, resp)
}

func buildServiceResponse(instances []*registry.ServiceInstance, minTTL int) []ServiceInstanceResponse {
	resp := make([]ServiceInstanceResponse, 0, len(instances))
	for _, inst := range instances {
		remaining := inst.RemainingTTL()
		if remaining <= 0 {
			continue
		}
		resp = append(resp, ServiceInstanceResponse{
			ID:            inst.ID,
			Address:       inst.Address,
			LastHeartbeat: inst.LastHeartbeat,
			Name:          inst.Name,
			Status:        inst.Status,
			RemainingTTL:  remaining.String(),
		})
	}
	return resp
}

func (h *Handler) QueryByPrefix(w http.ResponseWriter, r *http.Request) {
	prefixName := r.URL.Query().Get("prefix")
	if prefixName == "" {
		h.writeError(w, http.StatusBadRequest, "prefix parameter is required")
		return
	}

	names := h.store.ListNames()
	var matched []string
	for _, n := range names {
		if strings.HasPrefix(n, prefixName) {
			matched = append(matched, n)
		}
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"matched": matched,
		"count":   len(matched),
	})
}

func (h *Handler) SearchServices(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		h.writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	names := h.store.ListNames()
	var results []map[string]interface{}
	for _, name := range names {
		if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
			count := h.store.CountByName(name)
			results = append(results, map[string]interface{}{
				"name":  name,
				"count": count,
			})
		}
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"total":   len(results),
	})
}

func (h *Handler) GetServiceStats(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := fmt.Sprintf("/api/%s/service/", h.cfg.APIVersion)
	name := strings.TrimPrefix(path, prefix)

	if name == "" {
		h.writeError(w, http.StatusBadRequest, "service name is required")
		return
	}

	count := h.store.CountByName(name)
	instances := h.store.Get(name)
	expiredCount := 0
	now := time.Now()
	for _, inst := range instances {
		expireAt := inst.LastHeartbeat.Add(time.Duration(inst.TTLSeconds) * time.Second)
		if now.After(expireAt) {
			expiredCount++
		}
	}

	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":          name,
		"total":         count,
		"active":        count - expiredCount,
		"expired":       expiredCount,
		"checked_at":    now.Format(time.RFC3339),
	})
}

func (h *Handler) ListServiceInstances(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := fmt.Sprintf("/api/%s/service/", h.cfg.APIVersion)
	name := strings.TrimPrefix(path, prefix)

	if name == "" {
		h.writeError(w, http.StatusBadRequest, "service name is required")
		return
	}

	instances := h.store.Get(name)
	if len(instances) == 0 {
		h.writeJSON(w, http.StatusOK, []ServiceInstanceResponse{})
		return
	}

	detail := r.URL.Query().Get("detail")
	if detail == "true" {
		resp := make([]map[string]interface{}, 0, len(instances))
		for _, inst := range instances {
			resp = append(resp, map[string]interface{}{
				"id":              inst.ID,
				"name":            inst.Name,
				"address":         inst.Address,
				"ttl_seconds":     inst.TTLSeconds,
				"last_heartbeat":  inst.LastHeartbeat.Format(time.RFC3339),
				"registered_at":   inst.RegisteredAt.Format(time.RFC3339),
				"status":          inst.Status,
				"remaining_ttl":   inst.RemainingTTL().String(),
				"is_expired":      inst.IsExpired(),
			})
		}
		h.writeJSON(w, http.StatusOK, resp)
		return
	}

	resp := make([]ServiceInstanceResponse, 0, len(instances))
	for _, inst := range instances {
		resp = append(resp, ServiceInstanceResponse{
			ID:            inst.ID,
			Address:       inst.Address,
			LastHeartbeat: inst.LastHeartbeat,
			Name:          inst.Name,
			Status:        inst.Status,
		})
	}
	h.writeJSON(w, http.StatusOK, resp)
}

func sortedInstanceResponses(instances []*registry.ServiceInstance) []ServiceInstanceResponse {
	resp := make([]ServiceInstanceResponse, 0, len(instances))
	for _, inst := range instances {
		resp = append(resp, ServiceInstanceResponse{
			ID:            inst.ID,
			Address:       inst.Address,
			LastHeartbeat: inst.LastHeartbeat,
			Name:          inst.Name,
			Status:        inst.Status,
		})
	}
	return resp
}