package dto

import (
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/model"
)

// FileResponse is the public view of a file_object.
type FileResponse struct {
	UUID           uuid.UUID `json:"uuid"`
	Category       string    `json:"category"`
	OriginalName   string    `json:"original_name"`
	MimeType       string    `json:"mime_type"`
	SizeBytes      int64     `json:"size_bytes"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

func FileFromModel(o model.FileObject) FileResponse {
	return FileResponse{UUID: o.Uuid, Category: o.Category, OriginalName: o.OriginalName, MimeType: o.MimeType,
		SizeBytes: o.SizeBytes, ChecksumSHA256: o.ChecksumSha256, Status: o.Status, CreatedAt: o.CreatedAt}
}
