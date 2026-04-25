package admin

import (
	"encoding/json"
	"net/http"
)

// isWriteMethod returns true for HTTP methods that modify state.
func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// requireAdminRole is middleware that rejects state-changing requests from
// users who do not have the admin role. Read-only requests (GET, HEAD, OPTIONS)
// are always allowed through — the role check only applies to write methods.
//
// The role is read from the request context, set by AuthMiddleware during
// authentication. When no role is present (e.g., auth is disabled or the
// request is unauthenticated), RoleAdmin is assumed for backward compatibility.
func requireAdminRole(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWriteMethod(r.Method) {
			role := RoleFromContext(r.Context())
			if role != RoleAdmin {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				resp := ErrorResponse("FORBIDDEN", "admin role required for this operation")
				if err := json.NewEncoder(w).Encode(resp); err != nil {
					// Response already started; nothing more we can do.
				}
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
