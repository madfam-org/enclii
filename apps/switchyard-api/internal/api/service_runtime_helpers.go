package api

import "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"

func serviceExcludedFromRuntimeHealth(service *types.Service) bool {
	return service != nil && service.BuildConfig.BuildOnly
}
