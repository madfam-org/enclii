package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// TenantExportStore prevents the tenant export presigner/upload/delete surface
// from ever addressing the separate protected-backup prefix. The underlying
// native client and its legacy transport behavior are unchanged.
type TenantExportStore struct{ client *R2Client }

func (r *R2Client) TenantExportStorage() *TenantExportStore { return &TenantExportStore{r} }
func allowedTenantExportKey(key string) bool {
	key = strings.TrimLeft(key, "/")
	return key != "argocd-backups" && !strings.HasPrefix(key, "argocd-backups/")
}
func (s *TenantExportStore) Upload(ctx context.Context, key string, body io.Reader, contentType string) error {
	if s == nil || s.client == nil || !allowedTenantExportKey(key) {
		return errors.New("tenant export storage scope refused")
	}
	return s.client.Upload(ctx, key, body, contentType)
}
func (s *TenantExportStore) Delete(ctx context.Context, key string) error {
	if s == nil || s.client == nil || !allowedTenantExportKey(key) {
		return errors.New("tenant export storage scope refused")
	}
	return s.client.Delete(ctx, key)
}
func (s *TenantExportStore) GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if s == nil || s.client == nil || !allowedTenantExportKey(key) {
		return "", errors.New("tenant export storage scope refused")
	}
	return s.client.GetPresignedURL(ctx, key, expiry)
}
