package routes

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func serveSPAAsset(server http.Handler, assets fs.FS, w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	ext := path.Ext(name)
	if strings.HasPrefix(name, "assets/") && (ext == ".js" || ext == ".css") {
		info, err := fs.Stat(assets, name+".gz")
		if err == nil && !info.IsDir() {
			w.Header().Add("Vary", "Accept-Encoding")
			if r.Header.Get("Range") == "" && apiresponse.AcceptsGzip(r.Header.Values("Accept-Encoding")) {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Type", mime.TypeByExtension(ext))
				r = r.Clone(r.Context())
				r.URL.Path += ".gz"
			}
		}
	}
	server.ServeHTTP(w, r)
}

// spaEntryPath selects the build's authentication-first document for a cold
// application detail URL. Other routes and older builds use the regular document.
func spaEntryPath(assets fs.FS, requestPath string) string {
	if applicationID, ok := strings.CutPrefix(strings.TrimSuffix(requestPath, "/"), "/applications/"); ok &&
		applicationID != "" && !strings.Contains(applicationID, "/") {
		const workbenchDocument = "application-workbench.html"
		if info, err := fs.Stat(assets, workbenchDocument); err == nil && !info.IsDir() {
			return "/" + workbenchDocument
		}
	}
	return "/"
}
