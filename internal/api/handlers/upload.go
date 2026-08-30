package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaccess "github.com/opensoha/soha/internal/application/access"
)

const (
	brandingMaxFileSize = 2 << 20 // 2MB
)

var (
	allowedExtensions = map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".svg":  "image/svg+xml",
		".ico":  "image/x-icon",
		".webp": "image/webp",
	}
	alternateContentTypes = map[string]map[string]bool{
		".ico": {
			"image/vnd.microsoft.icon": true,
			"application/octet-stream": true,
		},
		".webp": {
			"application/octet-stream": true,
		},
	}
	contentSniffSize = 512
)

// UploadBrandingAsset handles branding image upload and returns a data URL for settings storage.
func (h *SettingsHandler) UploadBrandingAsset(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := appaccess.AuthorizeRuntimePermission(c.Request.Context(), h.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsBrandingManage, "update")); err != nil {
		apiresponse.Error(c, http.StatusForbidden, "access_denied", "missing branding manage permission")
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "missing file field")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > brandingMaxFileSize {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "file size exceeds 2MB limit")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	expectedContentType, ok := allowedExtensions[ext]
	if !ok {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "unsupported file type; allowed: jpg, png, svg, ico, webp")
		return
	}

	content, err := io.ReadAll(io.LimitReader(file, brandingMaxFileSize+1))
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid branding asset content")
		return
	}
	if len(content) > brandingMaxFileSize {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "file size exceeds 2MB limit")
		return
	}
	if len(content) == 0 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid branding asset content")
		return
	}

	if ext == ".svg" {
		content, err = sanitizeBrandingSVG(content)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "unsafe or unsupported SVG content")
			return
		}
	} else {
		sniff := content
		if len(sniff) > contentSniffSize {
			sniff = sniff[:contentSniffSize]
		}
		if !brandingContentTypeAllowed(ext, expectedContentType, http.DetectContentType(sniff)) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "file content does not match allowed image types")
			return
		}
	}

	apiresponse.Item(c, http.StatusOK, map[string]string{
		"url": "data:" + expectedContentType + ";base64," + base64.StdEncoding.EncodeToString(content),
	})
}

func brandingContentTypeAllowed(ext string, expected string, actual string) bool {
	actual = strings.TrimSpace(strings.ToLower(actual))
	expected = strings.TrimSpace(strings.ToLower(expected))
	if actual == expected {
		return true
	}
	return alternateContentTypes[ext][actual]
}

func sanitizeBrandingSVG(content []byte) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var output bytes.Buffer
	sanitizer := brandingSVGSanitizer{encoder: xml.NewEncoder(&output)}

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if err := sanitizer.finish(); err != nil {
				return nil, err
			}
			return output.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		if err := sanitizer.encode(token); err != nil {
			return nil, err
		}
	}
}

type brandingSVGSanitizer struct {
	encoder      *xml.Encoder
	depth        int
	elements     int
	skippedDepth int
	rootSeen     bool
}

func (s *brandingSVGSanitizer) finish() error {
	if !s.rootSeen || s.depth != 0 || s.skippedDepth != 0 {
		return errors.New("invalid SVG document")
	}
	return s.encoder.Flush()
}

func (s *brandingSVGSanitizer) encode(token xml.Token) error {
	switch item := token.(type) {
	case xml.Directive:
		return errors.New("SVG directives are not allowed")
	case xml.ProcInst:
		if s.rootSeen || !strings.EqualFold(item.Target, "xml") {
			return errors.New("SVG processing instructions are not allowed")
		}
	case xml.StartElement:
		return s.encodeStart(item)
	case xml.EndElement:
		return s.encodeEnd(item)
	case xml.CharData:
		return s.encodeCharData(item)
	}
	return nil
}

func (s *brandingSVGSanitizer) encodeStart(item xml.StartElement) error {
	name := strings.ToLower(item.Name.Local)
	if s.depth == 0 {
		if s.rootSeen || name != "svg" || !brandingSVGNamespaceAllowed(item.Name.Space) {
			return errors.New("SVG root element is required")
		}
		s.rootSeen = true
	}
	s.depth++
	s.elements++
	if s.depth > 64 || s.elements > 10000 {
		return errors.New("SVG document is too complex")
	}
	if s.skippedDepth > 0 {
		s.skippedDepth++
		return nil
	}
	if brandingSVGElementBlocked(name) {
		return errors.New("active SVG content is not allowed")
	}
	if !brandingSVGNamespaceAllowed(item.Name.Space) || !brandingSVGElementAllowed(name) {
		s.skippedDepth = 1
		return nil
	}
	cleaned, err := sanitizeBrandingSVGStart(item, name == "svg" && s.depth == 1)
	if err != nil {
		return err
	}
	return s.encoder.EncodeToken(cleaned)
}

func (s *brandingSVGSanitizer) encodeEnd(item xml.EndElement) error {
	if s.depth == 0 {
		return errors.New("invalid SVG document")
	}
	if s.skippedDepth > 0 {
		s.skippedDepth--
		s.depth--
		return nil
	}
	if err := s.encoder.EncodeToken(xml.EndElement{Name: xml.Name{Local: item.Name.Local}}); err != nil {
		return err
	}
	s.depth--
	return nil
}

func (s *brandingSVGSanitizer) encodeCharData(item xml.CharData) error {
	if s.depth == 0 {
		if strings.TrimSpace(string(item)) != "" {
			return errors.New("invalid SVG document")
		}
		return nil
	}
	if s.skippedDepth == 0 {
		return s.encoder.EncodeToken(item)
	}
	return nil
}

func sanitizeBrandingSVGStart(start xml.StartElement, root bool) (xml.StartElement, error) {
	cleaned := xml.StartElement{Name: xml.Name{Local: start.Name.Local}}
	if root {
		cleaned.Attr = append(cleaned.Attr, xml.Attr{Name: xml.Name{Local: "xmlns"}, Value: "http://www.w3.org/2000/svg"})
	}
	for _, attribute := range start.Attr {
		name := strings.ToLower(attribute.Name.Local)
		if name == "xmlns" || attribute.Name.Space == "xmlns" {
			continue
		}
		if name == "style" || name == "src" || strings.HasPrefix(name, "on") {
			return xml.StartElement{}, errors.New("active SVG attributes are not allowed")
		}
		if attribute.Name.Space != "" && name != "href" {
			continue
		}
		if err := validateBrandingSVGAttribute(name, attribute.Value); err != nil {
			return xml.StartElement{}, err
		}
		cleaned.Attr = append(cleaned.Attr, xml.Attr{Name: xml.Name{Local: attribute.Name.Local}, Value: attribute.Value})
	}
	return cleaned, nil
}

func validateBrandingSVGAttribute(name, value string) error {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if strings.Contains(trimmed, `\`) || strings.Contains(lower, "javascript:") || strings.Contains(lower, "vbscript:") ||
		strings.Contains(lower, "data:") || strings.Contains(lower, "file:") || strings.Contains(lower, "http:") ||
		strings.Contains(lower, "https:") || strings.Contains(lower, "//") || strings.Contains(lower, "@import") ||
		strings.Contains(lower, "@font-face") || strings.Contains(lower, "expression(") {
		return errors.New("external SVG references are not allowed")
	}
	if name == "href" && !brandingSVGLocalFragment(trimmed) {
		return errors.New("external SVG references are not allowed")
	}
	if strings.Contains(lower, "url(") && !brandingSVGUsesOnlyLocalURLs(lower) {
		return errors.New("external SVG references are not allowed")
	}
	return nil
}

func brandingSVGUsesOnlyLocalURLs(value string) bool {
	for {
		index := strings.Index(value, "url(")
		if index < 0 {
			return true
		}
		value = value[index+4:]
		end := strings.IndexByte(value, ')')
		if end < 0 {
			return false
		}
		target := strings.Trim(strings.TrimSpace(value[:end]), `"'`)
		if !brandingSVGLocalFragment(target) {
			return false
		}
		value = value[end+1:]
	}
}

func brandingSVGLocalFragment(value string) bool {
	return len(value) > 1 && value[0] == '#' && !strings.ContainsAny(value[1:], " \t\r\n\"'()<>/\\")
}

func brandingSVGNamespaceAllowed(namespace string) bool {
	return namespace == "" || namespace == "http://www.w3.org/2000/svg"
}

func brandingSVGElementBlocked(name string) bool {
	switch name {
	case "a", "animate", "animatemotion", "animatetransform", "audio", "canvas", "discard", "embed", "feimage", "foreignobject", "handler", "iframe", "image", "listener", "mpath", "object", "script", "set", "style", "video":
		return true
	default:
		return false
	}
}

func brandingSVGElementAllowed(name string) bool {
	switch name {
	case "svg", "g", "defs", "symbol", "use", "switch", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon", "text", "tspan", "textpath", "title", "desc", "lineargradient", "radialgradient", "stop", "clippath", "mask", "pattern", "marker", "filter", "feblend", "fecolormatrix", "fecomponenttransfer", "fecomposite", "feconvolvematrix", "fediffuselighting", "fedisplacementmap", "fedistantlight", "fedropshadow", "feflood", "fefunca", "fefuncb", "fefuncg", "fefuncr", "fegaussianblur", "femerge", "femergenode", "femorphology", "feoffset", "fepointlight", "fespecularlighting", "fespotlight", "fetile", "feturbulence":
		return true
	default:
		return false
	}
}
