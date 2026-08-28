package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DocsHandler агрегирует OpenAPI/Swagger трёх сервисов в одну точку на gateway.
// GET /docs            -> Swagger UI с dropdown (auth/metadata/upload/all)
// GET /openapi.json    -> merged OAS3 (paths + schemas всех сервисов)
// GET /openapi/auth.json, /openapi/metadata.json, /openapi/upload.json -> прокси 1-в-1
type DocsHandler struct {
	authURL     string
	metadataURL string
	uploadURL   string
	client      *http.Client
}

func NewDocsHandler(authURL, metadataURL, uploadURL string) *DocsHandler {
	return &DocsHandler{
		authURL:     authURL,
		metadataURL: metadataURL,
		uploadURL:   uploadURL,
		client:      &http.Client{Timeout: 5 * time.Second},
	}
}

// HandleDocsUI отдаёт HTML с SwaggerUIBundle urls.
func (h *DocsHandler) HandleDocsUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(docsHTML))
}

const docsHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>Flowix API — Swagger</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/>
<style>body{margin:0} #swagger-ui .topbar{display:none}</style>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
<script>
window.onload = () => {
  SwaggerUIBundle({
    urls: [
      {url: "/openapi.json", name: "all (merged)"},
      {url: "/openapi/auth.json", name: "auth"},
      {url: "/openapi/metadata.json", name: "metadata"},
      {url: "/openapi/upload.json", name: "upload"}
    ],
    dom_id: '#swagger-ui',
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    layout: "StandaloneLayout",
    docExpansion: "list",
    deepLinking: true,
    persistAuthorization: true,
    tryItOutEnabled: true
  });
};
</script>
</body>
</html>`

func (h *DocsHandler) HandleAuthSpec(w http.ResponseWriter, r *http.Request) {
	h.proxyJSON(w, h.authURL+"/openapi.json")
}

func (h *DocsHandler) HandleMetadataSpec(w http.ResponseWriter, r *http.Request) {
	h.proxyJSON(w, h.metadataURL+"/swagger/doc.json")
}

func (h *DocsHandler) HandleUploadSpec(w http.ResponseWriter, r *http.Request) {
	h.proxyJSON(w, h.uploadURL+"/swagger/doc.json")
}

func (h *DocsHandler) proxyJSON(w http.ResponseWriter, upstream string) {
	resp, err := h.client.Get(upstream)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"upstream fetch failed","upstream":%q}`, upstream), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// HandleMerged возвращает объединённый OAS3 документ (auth OAS3 + metadata/upload Swagger 2.0).
func (h *DocsHandler) HandleMerged(w http.ResponseWriter, r *http.Request) {
	authDoc := h.fetchMap(h.authURL + "/openapi.json")
	metaDoc := h.fetchMap(h.metadataURL + "/swagger/doc.json")
	uploadDoc := h.fetchMap(h.uploadURL + "/swagger/doc.json")

	merged := map[string]interface{}{
		"openapi": "3.0.1",
		"info": map[string]interface{}{
			"title":       "Flowix Gateway API",
			"version":     "1.0",
			"description": "Aggregated API: auth (FastAPI OAS3) + metadata (Go Swagger 2.0) + upload (Go Swagger 2.0) via gateway. Разделён по tags: auth / videos / upload. Единая точка: /docs",
		},
		"servers": []map[string]interface{}{{"url": "/"}},
		"paths":   map[string]interface{}{},
		"components": map[string]interface{}{
			"schemas":         map[string]interface{}{},
			"securitySchemes": map[string]interface{}{},
		},
	}
	paths := merged["paths"].(map[string]interface{})
	comps := merged["components"].(map[string]interface{})
	schemas := comps["schemas"].(map[string]interface{})
	schemes := comps["securitySchemes"].(map[string]interface{})

	// дефолтные Bearer схемы (покрываем оба стиля)
	schemes["BearerAuth"] = map[string]interface{}{"type": "http", "scheme": "bearer", "bearerFormat": "JWT", "description": `Type "Bearer {token}"`}
	schemes["HTTPBearer"] = map[string]interface{}{"type": "http", "scheme": "bearer"}

	if authDoc != nil {
		mergePaths(paths, authDoc)
		mergeSchemas(schemas, authDoc)
		mergeSecuritySchemes(schemes, authDoc)
	}
	if metaDoc != nil {
		mergePaths(paths, metaDoc)
		mergeDefinitionsToSchemas(schemas, metaDoc)
		mergeSecurityDefinitions(schemes, metaDoc)
	}
	if uploadDoc != nil {
		mergePaths(paths, uploadDoc)
		mergeDefinitionsToSchemas(schemas, uploadDoc)
		mergeSecurityDefinitions(schemes, uploadDoc)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(merged)
}

func (h *DocsHandler) fetchMap(url string) map[string]interface{} {
	resp, err := h.client.Get(url)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var m map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil
	}
	return m
}

func mergePaths(dst map[string]interface{}, src map[string]interface{}) {
	p, ok := src["paths"].(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range p {
		// не перетираем если уже есть (auth и metadata не пересекаются по путям)
		if _, exists := dst[k]; !exists {
			dst[k] = v
		} else {
			// мерджим методы внутри одного path (напр. /api/v1/videos GET+POST)
			if dstMethods, ok := dst[k].(map[string]interface{}); ok {
				if srcMethods, ok := v.(map[string]interface{}); ok {
					for mk, mv := range srcMethods {
						if _, exists := dstMethods[mk]; !exists {
							dstMethods[mk] = mv
						}
					}
				}
			}
		}
	}
}

func mergeSchemas(dst map[string]interface{}, src map[string]interface{}) {
	c, ok := src["components"].(map[string]interface{})
	if !ok {
		return
	}
	s, ok := c["schemas"].(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range s {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func mergeDefinitionsToSchemas(dst map[string]interface{}, src map[string]interface{}) {
	defs, ok := src["definitions"].(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range defs {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func mergeSecuritySchemes(dst map[string]interface{}, src map[string]interface{}) {
	c, ok := src["components"].(map[string]interface{})
	if !ok {
		return
	}
	s, ok := c["securitySchemes"].(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range s {
		if _, exists := dst[k]; !exists {
			dst[k] = v
		}
	}
}

func mergeSecurityDefinitions(dst map[string]interface{}, src map[string]interface{}) {
	defs, ok := src["securityDefinitions"].(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range defs {
		if _, exists := dst[k]; exists {
			continue
		}
		// Swagger 2.0 apiKey -> OAS3 http bearer
		if m, ok := v.(map[string]interface{}); ok {
			if m["type"] == "apiKey" && m["name"] == "Authorization" {
				dst[k] = map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
					"description":  m["description"],
				}
				continue
			}
		}
		dst[k] = v
	}
}
