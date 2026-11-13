package model

import "pfm-backend/internal/db/sqlc"

// File categories (file_object.category).
const (
	FileCategoryImage    = "image"
	FileCategoryDocument = "document"
)

// FileObject is a row of file_object.
type FileObject = sqlc.FileObject
