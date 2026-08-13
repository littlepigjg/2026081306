package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"service-registry/internal/config"
	"service-registry/internal/registry"
	"service-registry/pkg/idgen"
	"service-registry/pkg/logger"
)

type Handler struct {
	store  *registry.Store
	cfg    *config.Config
	log    *logger.Logger
	idGen  idgen.IDGenerator
}

func NewHandler(store *registry.Store, cfg *config.Config, log *logger.Logger) *Handler {
	return &Handler{
		store: store,
		cfg:   cfg,
		log:   log,
		idGen: idgen.NewGenerator(),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/register", h.withTimeout(h.HandleRegister, h.cfg.ReadTimeout))
	mux.HandleFunc("/api/v1/heartbeat", h.withTimeout(h.HandleHeartbeat, h.cfg.ReadTimeout))
	mux.HandleFunc(fmt.Sprintf("/api/%s/service/", h.cfg.APIVersion), h.withTimeout(h.HandleServiceRouting, h.cfg.ReadTimeout))
	mux.HandleFunc("/health", h.withTimeout(h.HandleHealth, 2*time.Second))
	mux.HandleFunc("/api/v1/services", h.withTimeout(h.HandleListAllServices, h.cfg.ReadTimeout))
}

func (h *Handler) withTimeout(handler http.HandlerFunc, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		r = r.WithContext(ctx)
		handler(w, r)
	}
}

type RegisterRequest struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	TTLSeconds int    `json:"ttl_seconds"`
}

type RegisterResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Address    string `json:"address"`
	TTLSeconds int    `json:"ttl"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

type HeartbeatRequest struct {
	ID string `json:"id"`
}

type ServiceInstanceResponse struct {
	ID             string    `json:"id"`
	Address        string    `json:"address"`
	LastHeartbeat  time.Time `json:"last_heartbeat"`
	Name           string    `json:"name,omitempty"`
	Status         string    `json:"status,omitempty"`
	RemainingTTL  string    `json:"remaining_ttl,omitempty"`
}

func (h *Handler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := validateRegisterRequest(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	inst := &registry.ServiceInstance{
		ID:            h.idGen.GenerateUUID(),
		Name:          req.Name,
		Address:       req.Address,
		TTLSeconds:    req.TTLSeconds,
		LastHeartbeat: time.Now(),
		RegisteredAt:  time.Now(),
		Status:        "active",
		Metadata:      map[string]string{},
	}

	if err := h.store.Register(inst); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to register: "+err.Error())
		return
	}

	resp := RegisterResponse{
		ID:         inst.ID,
		Name:       inst.Name,
		Address:    inst.Address,
		TTLSeconds: inst.TTLSeconds,
	}

	h.log.Info("Registered service: name=%s, id=%s, address=%s, ttl=%d",
		inst.Name, inst.ID, inst.Address, inst.TTLSeconds)

	h.writeJSON(w, http.StatusCreated, resp)
}

func validateRegisterRequest(req *RegisterRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if req.Address == "" {
		return fmt.Errorf("address is required")
	}
	if req.TTLSeconds <= 0 {
		return fmt.Errorf("ttl_seconds must be positive")
	}
	if req.TTLSeconds > 3600 {
		return fmt.Errorf("ttl_seconds exceeds maximum allowed value (3600)")
	}
	if !isValidAddress(req.Address) {
		return fmt.Errorf("address format is invalid, expected ip:port")
	}
	return nil
}

func isValidAddress(addr string) bool {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return false
	}
	port := parts[1]
	if port == "" || len(port) > 5 {
		return false
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (h *Handler) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.ID == "" {
		h.writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	inst, err := h.store.Heartbeat(req.ID)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "instance not found or expired: "+err.Error())
		return
	}

	h.log.Debug("Heartbeat received: id=%s, name=%s", inst.ID, inst.Name)

	resp := ServiceInstanceResponse{
		ID:            inst.ID,
		Address:       inst.Address,
		LastHeartbeat: inst.LastHeartbeat,
		Name:          inst.Name,
		Status:        inst.Status,
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleServiceRouting(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := fmt.Sprintf("/api/%s/service/", h.cfg.APIVersion)
	remaining := strings.TrimPrefix(path, prefix)

	if r.Method == http.MethodGet && !strings.Contains(remaining, "/") {
		h.GetServiceByName(w, r)
		return
	}

	if r.Method == http.MethodDelete && strings.Contains(remaining, "/") {
		h.DeregisterInstance(w, r)
		return
	}

	h.writeError(w, http.StatusMethodNotAllowed, "method not allowed or invalid path")
}

func (h *Handler) DeregisterInstance(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	prefix := fmt.Sprintf("/api/%s/service/", h.cfg.APIVersion)
	remaining := strings.TrimPrefix(path, prefix)

	parts := strings.SplitN(remaining, "/", 2)
	if len(parts) != 2 {
		h.writeError(w, http.StatusBadRequest, "invalid path format")
		return
	}

	name := parts[0]
	id := parts[1]

	if name == "" || id == "" {
		h.writeError(w, http.StatusBadRequest, "name and id are required")
		return
	}

	if h.store.Deregister(id) {
		h.log.Info("Deregistered instance: name=%s, id=%s", name, id)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.writeError(w, http.StatusNotFound, fmt.Sprintf("instance %s not found under service %s", id, name))
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	totalCount := h.store.Count()
	names := h.store.ListNames()

	resp := map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
		"instances": totalCount,
		"services":  len(names),
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleListAllServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	names := h.store.ListNames()
	resp := map[string]interface{}{
		"services": names,
		"total":    len(names),
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	resp := ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
	}
	h.writeJSON(w, status, resp)
}

func (h *Handler) Store() *registry.Store {
	return h.store
}