package appgateway

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"
)

type apiErrorBody struct {
	Error apiErrorDetail `json:"error"`
}

type apiErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiErrorBody{
		Error: apiErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}

func writePlatformStatus(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	if prefersJSONResponse(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.WriteHeader(status)
		if r == nil || r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(apiErrorBody{
				Error: apiErrorDetail{Code: code, Message: title},
			})
		}
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if r != nil && r.Method == http.MethodHead {
		return
	}
	page := buildPlatformHTMLPage(title, detail)
	_, _ = w.Write([]byte(page))
}

func buildPlatformHTMLPage(title, detail string) string {
	escapedTitle := html.EscapeString(title)
	escapedDetail := html.EscapeString(detail)
	return "<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
		"<title>" + escapedTitle + "</title>" +
		"<style>body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;margin:0;padding:64px 24px;background:#f8fafc;color:#0f172a}" +
		"main{max-width:480px;margin:0 auto;background:#fff;border:1px solid #e2e8f0;border-radius:12px;padding:32px}" +
		"h1{font-size:20px;margin:0 0 12px}p{font-size:14px;line-height:1.6;color:#475569;margin:0}</style>" +
		"</head><body><main><h1>" + escapedTitle + "</h1><p>" + escapedDetail + "</p></main></body></html>"
}

func prefersJSONResponse(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if accept == "" {
		return false
	}
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html") && !strings.Contains(accept, "*/*")
}

func shouldRedirectToLogin(r *http.Request) bool {
	if r == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	if prefersJSONResponse(r) {
		return false
	}
	fetchMode := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Mode")))
	if fetchMode == "navigate" {
		return true
	}
	if fetchMode != "" && fetchMode != "navigate" {
		return false
	}
	fetchDest := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Dest")))
	if fetchDest != "" && fetchDest != "document" {
		return false
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "text/html") {
		return true
	}
	return !isSubresourceAssetPath(r.URL.Path)
}

func isSubresourceAssetPath(urlPath string) bool {
	lower := strings.ToLower(strings.TrimSpace(urlPath))
	if lower == "" || strings.HasSuffix(lower, "/") || strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") {
		return false
	}
	lastSlash := strings.LastIndex(lower, "/")
	base := lower
	if lastSlash >= 0 {
		base = lower[lastSlash+1:]
	}
	dot := strings.LastIndex(base, ".")
	return dot > 0 && dot < len(base)-1
}
