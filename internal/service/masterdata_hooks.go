package service

import (
	"context"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/database"
)

// Hand-written business rules attached to scaffolded master data resources.

// checkTreeParent rejects a parent that is the row itself or one of its
// descendants (PRD §10.5: asset_class and sector are trees).
func checkTreeParent(ctx context.Context, d Deps, table, parentCol, field, parentUUID string, selfID *int64) error {
	parentID, err := database.RefID(ctx, d.TX, table, parentUUID, field)
	if err != nil || parentID == nil || selfID == nil {
		return err
	}
	cyc, err := database.WouldCreateCycle(ctx, d.TX, table, parentCol, *selfID, *parentID)
	if err != nil {
		return err
	}
	if cyc {
		return apperr.Invalid(field, "cycle", "induk tidak boleh data itu sendiri atau turunannya")
	}
	return nil
}

func checkAssetClass(ctx context.Context, d Deps, req *dto.AssetClassRequest, cur *sqlc.GetAssetClassByUUIDRow) error {
	var self *int64
	if cur != nil {
		self = &cur.ID
	}
	return checkTreeParent(ctx, d, "asset_class", "parent_asset_class_id", "parent_asset_class_uuid", req.ParentAssetClassUUID, self)
}

func checkSector(ctx context.Context, d Deps, req *dto.SectorRequest, cur *sqlc.GetSectorByUUIDRow) error {
	var self *int64
	if cur != nil {
		self = &cur.ID
	}
	return checkTreeParent(ctx, d, "sector", "parent_sector_id", "parent_sector_uuid", req.ParentSectorUUID, self)
}
