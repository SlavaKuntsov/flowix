package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
)

var hlsPathRe = regexp.MustCompile(`/hls/([0-9a-fA-F-]{36})`)

func extractVideoID(path string) string {
	m := hlsPathRe.FindStringSubmatch(path)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// GenerateHLSToken creates a short-lived JWT for HLS access to a private video.
func GenerateHLSToken(videoID, userID, secret string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":      userID,
		"video_id": videoID,
		"exp":      now.Add(time.Hour).Unix(),
		"iat":      now.Unix(),
		"type":     "hls",
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(secret))
}

// validateHLSToken checks ?token= JWT with video_id matching expected.
func validateHLSToken(tokenStr, expectedVideoID, secret string) (string, bool) {
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !tok.Valid {
		return "", false
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return "", false
	}
	if typ, _ := claims["type"].(string); typ != "hls" {
		return "", false
	}
	vid, _ := claims["video_id"].(string)
	if vid == "" || !strings.EqualFold(vid, expectedVideoID) {
		return "", false
	}
	sub, _ := claims["sub"].(string)
	// exp already validated by jwt.Parse
	return sub, true
}

// validateAccessToken extracts sub from Authorization Bearer if valid access token.
func validateAccessToken(r *http.Request, secret string) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" || !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	tokenStr := strings.TrimPrefix(h, "Bearer ")
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !tok.Valid {
		return "", false
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return "", false
	}
	if typ, _ := claims["type"].(string); typ != "" && typ != "access" {
		return "", false
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", false
	}
	return sub, true
}

type videoMeta struct {
	ID         string `json:"id"`
	OwnerID    string `json:"owner_id"`
	Visibility string `json:"visibility"`
	Status     string `json:"status"`
}

// fetchVideoMeta calls metadata internal endpoint to get visibility/owner.
func fetchVideoMeta(metadataURL, internalToken, videoID string) (*videoMeta, error) {
	url := strings.TrimSuffix(metadataURL, "/") + "/internal/videos/" + videoID
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if internalToken != "" {
		req.Header.Set("X-Internal-Token", internalToken)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata status %d", resp.StatusCode)
	}
	var v videoMeta
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	if v.Visibility == "" {
		v.Visibility = "public"
	}
	return &v, nil
}

// HLSAuth protects /hls/* : private videos require owner JWT or hls signed token.
func HLSAuth(jwtSecret, internalToken, metadataURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			videoID := extractVideoID(r.URL.Path)
			if videoID == "" {
				next.ServeHTTP(w, r)
				return
			}
			meta, err := fetchVideoMeta(metadataURL, internalToken, videoID)
			if err != nil {
				// If video not found, let vod return 404; if fetch fails, propagate 502
				// To avoid leaking existence, treat not-found as 404 from vod.
				// If metadata unavailable, return 502.
				if strings.Contains(err.Error(), "404") {
					next.ServeHTTP(w, r)
					return
				}
				// Allow through on transient failure? Better 502.
				http.Error(w, `{"error":"upstream unavailable"}`, http.StatusBadGateway)
				return
			}
			vis := meta.Visibility
			if vis == "" {
				vis = "public"
			}
			isPublic := vis == "public" || vis == "unlisted"
			if isPublic {
				// public/unlisted: allow, set public cache header via wrapped writer
				rw := &hlsCacheWriter{ResponseWriter: w, visibility: vis}
				next.ServeHTTP(rw, r)
				return
			}
			// private: need auth
			// check hls token first (query or header for hls.js xhrSetup)
			tokenParam := r.URL.Query().Get("token")
			if tokenParam == "" {
				tokenParam = r.Header.Get("X-HLS-Token")
			}
			if tokenParam != "" {
				if sub, ok := validateHLSToken(tokenParam, videoID, jwtSecret); ok && sub == meta.OwnerID {
					rw := &hlsCacheWriter{ResponseWriter: w, visibility: vis}
					next.ServeHTTP(rw, r)
					return
				}
			}
			if sub, ok := validateAccessToken(r, jwtSecret); ok && sub == meta.OwnerID {
				rw := &hlsCacheWriter{ResponseWriter: w, visibility: vis}
				// forward user for logging/proxy
				r.Header.Set("X-User-ID", sub)
				next.ServeHTTP(rw, r)
				return
			}
			http.Error(w, `{"error":"forbidden: private video"}`, http.StatusForbidden)
		})
	}
}

type hlsCacheWriter struct {
	http.ResponseWriter
	visibility string
	wrote      bool
}

func (h *hlsCacheWriter) WriteHeader(code int) {
	if !h.wrote {
		h.wrote = true
		if h.visibility == "private" {
			h.Header().Set("Cache-Control", "private, max-age=10")
			h.Header().Set("CDN-Cache-Control", "private, max-age=10")
			h.Header().Set("Surrogate-Control", "max-age=10")
		} else {
			// for public we keep upstream's cache but ensure public
			if h.Header().Get("Cache-Control") == "" {
				h.Header().Set("Cache-Control", "public, max-age=86400")
			}
		}
	}
	h.ResponseWriter.WriteHeader(code)
}

func (h *hlsCacheWriter) Write(b []byte) (int, error) {
	if !h.wrote {
		h.WriteHeader(http.StatusOK)
	}
	return h.ResponseWriter.Write(b)
}

// HLSTokenHandler returns a signed token for HLS private access. Requires auth.
func HLSTokenHandler(jwtSecret, internalToken, metadataURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		videoID := chi.URLParam(r, "id")
		if videoID == "" {
			videoID = r.URL.Query().Get("id")
		}
		if videoID == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) >= 4 {
				for i, p := range parts {
					if p == "videos" && i+1 < len(parts) {
						videoID = parts[i+1]
						break
					}
				}
			}
		}
		userID := UserIDFromCtx(r.Context())
		if userID == "" {
			// try via Authorization parsing as fallback (if middleware not applied)
			if sub, ok := validateAccessToken(r, jwtSecret); ok {
				userID = sub
			}
		}
		if userID == "" {
			http.Error(w, `{"error":"missing token"}`, http.StatusUnauthorized)
			return
		}
		if videoID == "" {
			http.Error(w, `{"error":"missing video id"}`, http.StatusBadRequest)
			return
		}
		meta, err := fetchVideoMeta(metadataURL, internalToken, videoID)
		if err != nil {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		if meta.OwnerID != userID {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		token, err := GenerateHLSToken(videoID, userID, jwtSecret)
		if err != nil {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      token,
			"expires_in": 3600,
			"url":        fmt.Sprintf("/hls/%s/master.m3u8?token=%s", videoID, token),
		})
	}
}
