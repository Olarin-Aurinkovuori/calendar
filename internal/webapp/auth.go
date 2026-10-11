package webapp

import (
	"log"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

// requireAuth wraps next with an authentication check against the users
// configured in the YAML file (config.AuthConfig.Users, or the external
// file it points to, bcrypt hashes).
//
// If the server was started with a trusted proxy-user query parameter (see
// New), a request that carries that parameter is treated as already
// authenticated by a frontend reverse proxy: the supplied username is
// trusted outright, without a password check, as long as it matches a
// configured user. This is meant only for deployments where the proxy
// itself verifies the caller's identity and rewrites/appends the parameter
// -- it must not be reachable directly by untrusted clients. Requests
// without the parameter fall back to normal HTTP Basic Auth.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.trustProxyUserParam != "" {
			if username := r.URL.Query().Get(s.trustProxyUserParam); username != "" {
				if s.isKnownUser(username) {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "unknown proxy-supplied user", http.StatusForbidden)
				return
			}
		}
		user, pass, ok := r.BasicAuth()
		if ok && s.checkCredentials(user, pass) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="calendar config", charset="UTF-8"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
	})
}

// isKnownUser reports whether username matches one of the configured admin
// users, without checking any password. Used only for the trusted-proxy
// authentication path.
func (s *Server) isKnownUser(username string) bool {
	users, err := s.store.EffectiveUsers()
	if err != nil {
		log.Printf("auth: reading users: %v", err)
		return false
	}
	for _, u := range users {
		if u.Username == username {
			return true
		}
	}
	return false
}

func (s *Server) checkCredentials(username, password string) bool {
	users, err := s.store.EffectiveUsers()
	if err != nil {
		log.Printf("auth: reading users: %v", err)
		return false
	}
	var hash string
	var found bool
	for _, u := range users {
		if u.Username == username {
			hash = u.PasswordHash
			found = true
			break
		}
	}
	if !found {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
