package api

import (
	"fmt"
	"sort"
	"strings"
)

func operationNamespace(req operatorOperationRequest, fallback string) string {
	if req.Scope != nil && strings.TrimSpace(req.Scope["namespace"]) != "" {
		return strings.TrimSpace(req.Scope["namespace"])
	}
	return fallback
}

func operationTarget(req operatorOperationRequest) string {
	if req.Args == nil {
		return ""
	}
	return strings.TrimSpace(req.Args["target"])
}

func operationArg(req operatorOperationRequest, names ...string) string {
	if req.Args == nil {
		return ""
	}
	for _, name := range names {
		if value := strings.TrimSpace(req.Args[name]); value != "" {
			return value
		}
	}
	return ""
}

func stringSliceFromAny(value any) []string {
	values := []string{}
	switch typed := value.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				values = append(values, text)
			}
		}
	}
	sort.Strings(values)
	return values
}

func mapStringValue(data map[string]any, key string) string {
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mapBoolValue(data map[string]any, key string) bool {
	value, ok := data[key]
	if !ok || value == nil {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}
