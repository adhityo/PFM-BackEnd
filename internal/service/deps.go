package service

import "pfm-backend/internal/platform/database"

// Deps are the shared dependencies of scaffolded resources and their hooks.
// TX is pfm_core, where every scaffolded table lives; Security is pfm_security,
// used only for cross-database integrity checks (no FK across databases).
type Deps struct {
	TX       *database.TxManager
	Security *database.TxManager
	Files    *FileService
}
