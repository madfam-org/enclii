package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type BareMetalHostRepository struct {
	db DBTX
}

func NewBareMetalHostRepository(db DBTX) *BareMetalHostRepository {
	return &BareMetalHostRepository{db: db}
}

func NewBareMetalHostRepositoryWithTx(tx DBTX) *BareMetalHostRepository {
	return &BareMetalHostRepository{db: tx}
}

func (r *BareMetalHostRepository) Create(ctx context.Context, h *types.BareMetalHost) error {
	h.ID = uuid.New()
	query := `INSERT INTO bare_metal_hosts (id, name, cluster_id, bmc_address, bmc_credentials_ref, mac_address, boot_mode, state, power_state, hardware_profile, firmware_version, root_device_hints, raid_config, cost_per_hour_cents)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING created_at, updated_at`
	hw, _ := json.Marshal(h.HardwareProfile)
	rdh, _ := json.Marshal(h.RootDeviceHints)
	rc, _ := json.Marshal(h.RAIDConfig)
	return r.db.QueryRowContext(ctx, query,
		h.ID, h.Name, h.ClusterID, h.BMCAddress, h.BMCCredentialsRef, h.MACAddress, h.BootMode, h.State, h.PowerState, hw, h.FirmwareVersion, rdh, rc, h.CostPerHourCents,
	).Scan(&h.CreatedAt, &h.UpdatedAt)
}

func (r *BareMetalHostRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.BareMetalHost, error) {
	query := `SELECT id, name, cluster_id, bmc_address, bmc_credentials_ref, mac_address, boot_mode, state, power_state, hardware_profile, firmware_version, root_device_hints, raid_config, cost_per_hour_cents, last_inspection_at, created_at, updated_at
		FROM bare_metal_hosts WHERE id = $1`
	h := &types.BareMetalHost{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&h.ID, &h.Name, &h.ClusterID, &h.BMCAddress, &h.BMCCredentialsRef, &h.MACAddress, &h.BootMode, &h.State, &h.PowerState, &h.HardwareProfile, &h.FirmwareVersion, &h.RootDeviceHints, &h.RAIDConfig, &h.CostPerHourCents, &h.LastInspectionAt, &h.CreatedAt, &h.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get bare metal host: %w", err)
	}
	return h, nil
}

func (r *BareMetalHostRepository) List(ctx context.Context) ([]*types.BareMetalHost, error) {
	query := `SELECT id, name, cluster_id, bmc_address, bmc_credentials_ref, mac_address, boot_mode, state, power_state, hardware_profile, firmware_version, root_device_hints, raid_config, cost_per_hour_cents, last_inspection_at, created_at, updated_at
		FROM bare_metal_hosts ORDER BY name`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list bare metal hosts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var hosts []*types.BareMetalHost
	for rows.Next() {
		h := &types.BareMetalHost{}
		if err := rows.Scan(&h.ID, &h.Name, &h.ClusterID, &h.BMCAddress, &h.BMCCredentialsRef, &h.MACAddress, &h.BootMode, &h.State, &h.PowerState, &h.HardwareProfile, &h.FirmwareVersion, &h.RootDeviceHints, &h.RAIDConfig, &h.CostPerHourCents, &h.LastInspectionAt, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan bare metal host: %w", err)
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func (r *BareMetalHostRepository) UpdateState(ctx context.Context, id uuid.UUID, state types.BMHState, powerState types.BMHPowerState) error {
	_, err := r.db.ExecContext(ctx, `UPDATE bare_metal_hosts SET state=$2, power_state=$3 WHERE id=$1`, id, state, powerState)
	if err != nil {
		return fmt.Errorf("failed to update BMH state: %w", err)
	}
	return nil
}

func (r *BareMetalHostRepository) UpdateHardwareProfile(ctx context.Context, id uuid.UUID, profile json.RawMessage) error {
	_, err := r.db.ExecContext(ctx, `UPDATE bare_metal_hosts SET hardware_profile=$2, last_inspection_at=NOW() WHERE id=$1`, id, profile)
	if err != nil {
		return fmt.Errorf("failed to update hardware profile: %w", err)
	}
	return nil
}
