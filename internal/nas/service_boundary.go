package nas

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// serviceBoundary 只统一服务身份与浏览器隔离；各服务保留自己的路由、方法和超时策略。
// 不复用管理 nonce，不信任转发头，不在身份核对前读取请求体或执行下游处理器。
func serviceBoundary(token, host string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte("Bearer " + token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if len(token) < 32 || r.Host != host || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			apiError(w, http.StatusUnauthorized, "service_unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
