// Package handler implements HTTP handlers of the upload service
// (upload, presign, resumable, ownership checks).
package handler

import (
	"net/http"
	"strings"
)

// OwnershipChecker resolves the video owner via metadata internal API.
// Required before any write to raw/{id}/* — presign/complete/resumable
// used to trust the video UUID from the URL (IDOR, issue #46).
type OwnershipChecker interface {
	GetVideoOwner(videoID string) (string, error)
}

// requireOwnership verifies userID owns videoID. On failure it writes the
// response (403 wrong owner, 404 unknown video, 502 metadata unavailable —
// fail closed) and returns false.
func requireOwnership(checker OwnershipChecker, videoID, userID string, w http.ResponseWriter) bool {
	owner, err := checker.GetVideoOwner(videoID)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			http.Error(w, `{"error":"video not found"}`, http.StatusNotFound)
			return false
		}
		http.Error(w, `{"error":"metadata unavailable"}`, http.StatusBadGateway)
		return false
	}
	if owner != userID {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return false
	}
	return true
}
