package services

import (
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// CostTrackingService handles infrastructure cost attribution
type CostTrackingService struct {
	repos  *db.Repositories
	logger *logrus.Logger
}

func NewCostTrackingService(repos *db.Repositories, logger *logrus.Logger) *CostTrackingService {
	return &CostTrackingService{repos: repos, logger: logger}
}
