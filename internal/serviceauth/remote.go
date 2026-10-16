package serviceauth

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

// RemoteAccess is an explicit opt-in for the unprivileged desktop gateway.
// Hosts are exact HTTP authorities, including a port when clients send one.
func RemoteAccess(hosts, key string) (func(http.Handler) http.Handler, error) {
	if !validKey(key) {
		return nil, errors.New("remote gateway requires MANAGED_LLAMA_REMOTE_KEY containing 64 hexadecimal characters")
	}
	allowed := map[string]bool{}
	for _, value := range strings.Split(hosts, ",") {
		host := strings.ToLower(strings.TrimSpace(value))
		if host == "" || strings.ContainsAny(host, "/\\@?#* \t\r\n") {
			return nil, errors.New("MANAGED_LLAMA_REMOTE_HOSTS requires exact host names, optionally with ports")
		}
		allowed[host] = true
	}
	return func(next http.Handler) http.Handler {
		authenticated := requireKey(key, next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.Host)
			local := err == nil && (strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback())
			if !local && !allowed[strings.ToLower(r.Host)] {
				http.Error(w, "invalid remote gateway host", http.StatusForbidden)
				return
			}
			authenticated.ServeHTTP(w, r)
		})
	}, nil
}
