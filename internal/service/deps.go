package service

import "pfm-backend/internal/platform/database"

// Deps are the shared dependencies of scaffolded resources and their hooks.
type Deps struct {
	TX    *database.TxManager
	Files *FileService
}
