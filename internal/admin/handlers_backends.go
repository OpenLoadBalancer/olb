package admin

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strings"

	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/pkg/errors"
)

// listBackends handles GET /api/v1/backends
func (s *Server) listBackends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.poolManager == nil {
		writeSuccess(w, []string{})
		return
	}

	pools := s.poolManager.GetAllPools()
	names := make([]string, 0, len(pools))
	for _, pool := range pools {
		names = append(names, pool.Name)
	}

	writeSuccess(w, names)
}

// getPool handles GET /api/v1/backends/:pool
func (s *Server) getPool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	poolName := extractPoolName(r.URL.Path)
	if poolName == "" {
		writeError(w, http.StatusBadRequest, "INVALID_POOL", "pool name is required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	response := poolToInfo(pool)
	writeSuccess(w, response)
}

// addBackend handles POST /api/v1/backends/:pool
func (s *Server) addBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}

	poolName := extractPoolName(r.URL.Path)
	if poolName == "" {
		writeError(w, http.StatusBadRequest, "INVALID_POOL", "pool name is required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	var req AddBackendRequest
	if err := readJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON")
		return
	}

	// Validate required fields
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELD", "backend ID is required")
		return
	}
	if req.Address == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELD", "backend address is required")
		return
	}

	// Validate address format (host:port)
	if _, err := net.ResolveTCPAddr("tcp", req.Address); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ADDRESS", "backend address must be in host:port format")
		return
	}

	// Check if backend already exists
	if existing := pool.GetBackend(req.ID); existing != nil {
		writeError(w, http.StatusConflict, "ALREADY_EXISTS", "backend already exists: "+req.ID)
		return
	}

	// Raft mode: propose the backend addition through consensus
	if s.raftProposer != nil {
		backendJSON, err := json.Marshal(map[string]any{
			"id":      req.ID,
			"address": req.Address,
			"weight":  req.Weight,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to marshal backend data")
			return
		}
		if err := s.raftProposer.ProposeUpdateBackend(poolName, backendJSON); err != nil {
			writeError(w, http.StatusConflict, "RAFT_ERROR",
				"failed to propose backend addition")
			return
		}
		writeSuccess(w, map[string]string{
			"status":  "proposed",
			"pool":    poolName,
			"backend": req.ID,
		})
		return
	}

	// Create backend (standalone mode)
	b := backend.NewBackend(req.ID, req.Address)
	if req.Weight > 0 {
		if req.Weight > math.MaxInt32 {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "weight exceeds maximum value")
			return
		}
		b.Weight = int32(req.Weight)
	}

	if err := pool.AddBackend(b); err != nil {
		if errors.Is(err, errors.ErrAlreadyExist) {
			writeError(w, http.StatusConflict, "ALREADY_EXISTS", "backend already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
		return
	}

	writeSuccess(w, backendToInfo(b))
}

// removeBackend handles DELETE /api/v1/backends/:pool/:backend
func (s *Server) removeBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only DELETE is allowed")
		return
	}

	poolName, backendID := extractBackendID(r.URL.Path)
	if poolName == "" || backendID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "pool and backend IDs are required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	// Raft mode: propose the backend removal through consensus
	if s.raftProposer != nil {
		if err := s.raftProposer.ProposeDeleteBackend(poolName, backendID); err != nil {
			writeError(w, http.StatusConflict, "RAFT_ERROR",
				"failed to propose backend removal")
			return
		}
		writeSuccess(w, map[string]string{
			"status":  "proposed",
			"pool":    poolName,
			"backend": backendID,
		})
		return
	}

	// Standalone mode: direct removal
	if err := pool.RemoveBackend(backendID); err != nil {
		if errors.Is(err, errors.ErrBackendNotFound) {
			writeError(w, http.StatusNotFound, "BACKEND_NOT_FOUND", "backend not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
		return
	}

	writeSuccess(w, map[string]string{"message": "backend removed successfully"})
}

// updateBackend handles PATCH /api/v1/backends/:pool/:backend
func (s *Server) updateBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only PATCH is allowed")
		return
	}

	poolName, backendID := extractBackendID(r.URL.Path)
	if poolName == "" || backendID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "pool and backend IDs are required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	b := pool.GetBackend(backendID)
	if b == nil {
		writeError(w, http.StatusNotFound, "BACKEND_NOT_FOUND", "backend not found: "+backendID)
		return
	}

	var req UpdateBackendRequest
	if err := readJSONBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON")
		return
	}

	if req.Weight != nil {
		if *req.Weight < 0 || *req.Weight > 1000 {
			writeError(w, http.StatusBadRequest, "INVALID_WEIGHT", "weight must be between 0 and 1000")
			return
		}
	}

	// Raft mode: propose the backend update through consensus
	if s.raftProposer != nil {
		backendJSON, err := json.Marshal(map[string]any{
			"id":      backendID,
			"address": b.Address,
			"weight":  b.Weight,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MARSHAL_ERROR", "failed to marshal backend data")
			return
		}
		if req.Weight != nil {
			backendJSON, err = json.Marshal(map[string]any{
				"id":      backendID,
				"address": b.Address,
				"weight":  *req.Weight,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "MARSHAL_ERROR", "failed to marshal backend data")
				return
			}
		}
		if err := s.raftProposer.ProposeUpdateBackend(poolName, backendJSON); err != nil {
			writeError(w, http.StatusConflict, "RAFT_ERROR",
				"failed to propose backend update")
			return
		}
		writeSuccess(w, map[string]string{
			"status":  "proposed",
			"pool":    poolName,
			"backend": backendID,
		})
		return
	}

	// Standalone mode: direct update
	if req.Weight != nil {
		b.Weight = *req.Weight
	}
	if req.MaxConns != nil {
		if *req.MaxConns < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_MAX_CONNS", "max connections must be non-negative")
			return
		}
		b.MaxConns = *req.MaxConns
	}

	writeSuccess(w, backendToInfo(b))
}

// drainBackend handles POST /api/v1/backends/:pool/:backend/drain
func (s *Server) drainBackend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}

	// Extract pool and backend from path like /api/v1/backends/:pool/:backend/drain
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 6 {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "pool and backend IDs are required")
		return
	}

	poolName := parts[3]
	backendID := parts[4]

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	if err := pool.DrainBackend(backendID); err != nil {
		if errors.Is(err, errors.ErrBackendNotFound) {
			writeError(w, http.StatusNotFound, "BACKEND_NOT_FOUND", "backend not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
		return
	}

	writeSuccess(w, map[string]string{"message": "backend drained successfully"})
}

// getBackendDetail handles GET /api/v1/backends/:pool/:backend
func (s *Server) getBackendDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	poolName, backendID := extractBackendID(r.URL.Path)
	if poolName == "" || backendID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "pool and backend IDs are required")
		return
	}

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	b := pool.GetBackend(backendID)
	if b == nil {
		writeError(w, http.StatusNotFound, "BACKEND_NOT_FOUND", "backend not found: "+backendID)
		return
	}

	writeSuccess(w, backendToInfo(b))
}
