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
// fail closed) and returns false. Issue #59: ошибки через writeError (JSON,
// детали — только в лог).
func requireOwnership(checker OwnershipChecker, videoID, userID string, w http.ResponseWriter, r *http.Request) bool {
	owner, err := checker.GetVideoOwner(videoID)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			writeError(w, r, http.StatusNotFound, "video not found", err)
			return false
		}
		writeError(w, r, http.StatusBadGateway, "metadata unavailable", err)
		return false
	}
	if owner != userID {
		writeError(w, r, http.StatusForbidden, "forbidden", nil)
		return false
	}
	return true
}
