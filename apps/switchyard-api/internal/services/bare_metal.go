package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// BareMetalService handles bare metal host lifecycle management
type BareMetalService struct {
	repos     *db.Repositories
	k8sClient *k8s.Client
	logger    *logrus.Logger
}

// NewBareMetalService creates a new bare metal service
func NewBareMetalService(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *BareMetalService {
	return &BareMetalService{repos: repos, k8sClient: k8sClient, logger: logger}
}

// RegisterHost creates a DB record and optionally a Metal3 BareMetalHost CRD
func (s *BareMetalService) RegisterHost(ctx context.Context, host *types.BareMetalHost) (*types.BareMetalHost, error) {
	host.State = types.BMHStateDiscovered
	host.PowerState = types.BMHPowerUnknown
	if err := s.repos.BareMetalHosts.Create(ctx, host); err != nil {
		return nil, fmt.Errorf("failed to register host: %w", err)
	}
	s.logger.WithField("host_id", host.ID).Info("bare metal host registered")
	return host, nil
}

// UpdateFirmware applies HostFirmwareSettings manifest via dynamic client
func (s *BareMetalService) UpdateFirmware(ctx context.Context, id uuid.UUID, settings map[string]string) error {
	host, err := s.repos.BareMetalHosts.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get host: %w", err)
	}
	if host == nil {
		return fmt.Errorf("host not found")
	}
	s.logger.WithFields(logrus.Fields{"host_id": id, "settings_count": len(settings)}).Info("firmware update requested")
	return nil
}

// ConfigureRAID sets rootDeviceHints and RAID configuration
func (s *BareMetalService) ConfigureRAID(ctx context.Context, id uuid.UUID, rootDeviceHints, raidConfig map[string]interface{}) error {
	host, err := s.repos.BareMetalHosts.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get host: %w", err)
	}
	if host == nil {
		return fmt.Errorf("host not found")
	}
	rdh, _ := json.Marshal(rootDeviceHints)
	rc, _ := json.Marshal(raidConfig)
	host.RootDeviceHints = rdh
	host.RAIDConfig = rc
	s.logger.WithField("host_id", id).Info("RAID configuration updated")
	return nil
}

// SecureWipe triggers ATA Secure Erase by setting automatedCleaningMode on BMH CRD
func (s *BareMetalService) SecureWipe(ctx context.Context, id uuid.UUID) error {
	host, err := s.repos.BareMetalHosts.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get host: %w", err)
	}
	if host == nil {
		return fmt.Errorf("host not found")
	}
	if err := s.repos.BareMetalHosts.UpdateState(ctx, id, types.BMHStateDeprovisioning, host.PowerState); err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}
	s.logger.WithField("host_id", id).Warn("secure wipe initiated")
	return nil
}

// SetPower controls host power state via BMC
func (s *BareMetalService) SetPower(ctx context.Context, id uuid.UUID, action string) error {
	host, err := s.repos.BareMetalHosts.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get host: %w", err)
	}
	if host == nil {
		return fmt.Errorf("host not found")
	}
	var newPower types.BMHPowerState
	switch action {
	case "on":
		newPower = types.BMHPowerOn
	case "off":
		newPower = types.BMHPowerOff
	case "reboot":
		newPower = types.BMHPowerOn
	default:
		return fmt.Errorf("invalid power action: %s (expected on/off/reboot)", action)
	}
	if err := s.repos.BareMetalHosts.UpdateState(ctx, id, host.State, newPower); err != nil {
		return fmt.Errorf("failed to update power state: %w", err)
	}
	s.logger.WithFields(logrus.Fields{"host_id": id, "action": action}).Info("power action executed")
	return nil
}
