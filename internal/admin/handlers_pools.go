package admin

import (
	"net/http"
	"strings"
)

// listPools handles GET /api/v1/pools
func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}

	if s.poolManager == nil {
		writeSuccess(w, []PoolInfo{})
		return
	}

	pools := s.poolManager.GetAllPools()
	response := make([]PoolInfo, 0, len(pools))
	for _, pool := range pools {
		response = append(response, poolToInfo(pool))
	}

	writeSuccess(w, response)
}

// handlePoolDetail handles requests to /api/v1/pools/...
func (s *Server) handlePoolDetail(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")

	// /api/v1/pools/:pool (4 parts)
	if len(parts) == 4 {
		switch r.Method {
		case http.MethodGet:
			s.getPoolByName(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		}
	} else {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "invalid path")
	}
}

// getPoolByName handles GET /api/v1/pools/:pool
func (s *Server) getPoolByName(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		writeError(w, http.StatusBadRequest, "INVALID_POOL", "pool name is required")
		return
	}
	poolName := parts[3]

	if s.poolManager == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found")
		return
	}

	pool := s.poolManager.GetPool(poolName)
	if pool == nil {
		writeError(w, http.StatusNotFound, "POOL_NOT_FOUND", "pool not found: "+poolName)
		return
	}

	writeSuccess(w, poolToInfo(pool))
}
