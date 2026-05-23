package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

type serviceVolume struct {
	Name             string `json:"name"`
	MountPath        string `json:"mount_path"`
	Size             string `json:"size"`
	StorageClassName string `json:"storage_class_name,omitempty"`
	AccessMode       string `json:"access_mode,omitempty"`
}

type serviceSettingsResponse struct {
	Settings struct {
		Volumes []serviceVolume `json:"volumes"`
	} `json:"settings"`
}

// NewVolumesCommand manages persistent volumes on a service.
func NewVolumesCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "volumes",
		Aliases: []string{"volume", "storage", "pvc"},
		Short:   "Manage persistent volumes for a service",
		Long: `Manage persistent volume attachments for a service.

Volumes are persisted on the service record and applied on the next deploy
(reconciler creates PVCs and mounts them into the workload).

Examples:
  enclii volumes list
  enclii volumes set --file volumes.json
  enclii volumes add --name data --mount-path /data --size 10Gi`,
	}

	cmd.AddCommand(newVolumesListCommand(cfg))
	cmd.AddCommand(newVolumesSetCommand(cfg))
	cmd.AddCommand(newVolumesAddCommand(cfg))
	cmd.AddCommand(newVolumesClearCommand(cfg))

	return cmd
}

func newVolumesListCommand(cfg *config.Config) *cobra.Command {
	var serviceName, specFile string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List persistent volumes for a service",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runVolumesList(cfg, serviceName, specFile)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")

	return cmd
}

func newVolumesSetCommand(cfg *config.Config) *cobra.Command {
	var serviceName, specFile, volumesFile string

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Replace all volumes from a JSON file",
		Long: `Replace all volumes on a service from a JSON array file.

File format:
[
  {
    "name": "data",
    "mount_path": "/data",
    "size": "10Gi",
    "storage_class_name": "longhorn",
    "access_mode": "ReadWriteOnce"
  }
]`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runVolumesSet(cfg, serviceName, specFile, volumesFile)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")
	cmd.Flags().StringVar(&volumesFile, "volumes-file", "", "JSON file with volume array (required)")

	return cmd
}

func newVolumesAddCommand(cfg *config.Config) *cobra.Command {
	var serviceName, specFile string
	var name, mountPath, size, storageClass, accessMode string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Append a volume (or replace if name exists)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runVolumesAdd(cfg, serviceName, specFile, name, mountPath, size, storageClass, accessMode)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")
	cmd.Flags().StringVar(&name, "name", "", "Volume name (required)")
	cmd.Flags().StringVar(&mountPath, "mount-path", "", "Mount path inside container (required)")
	cmd.Flags().StringVar(&size, "size", "10Gi", "Volume size")
	cmd.Flags().StringVar(&storageClass, "storage-class", "longhorn", "Storage class")
	cmd.Flags().StringVar(&accessMode, "access-mode", "ReadWriteOnce", "Access mode")

	return cmd
}

func newVolumesClearCommand(cfg *config.Config) *cobra.Command {
	var serviceName, specFile string

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Remove all volumes from a service",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runVolumesClear(cfg, serviceName, specFile)
		},
	}

	cmd.Flags().StringVarP(&serviceName, "service", "s", "", "Service name")
	cmd.Flags().StringVarP(&specFile, "file", "f", "service.yaml", "Path to service.yaml")

	return cmd
}

func runVolumesList(cfg *config.Config, serviceName, specFile string) error {
	ctx := context.Background()
	service, _, err := resolveService(ctx, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	volumes, err := fetchServiceVolumes(ctx, cfg, service.ID.String())
	if err != nil {
		return err
	}

	if len(volumes) == 0 {
		fmt.Println("No persistent volumes configured")
		fmt.Println("💡 Add one with: enclii volumes add --name data --mount-path /data --size 10Gi")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tMOUNT\tSIZE\tCLASS\tACCESS")
	for _, v := range volumes {
		className := v.StorageClassName
		if className == "" {
			className = "standard"
		}
		mode := v.AccessMode
		if mode == "" {
			mode = "ReadWriteOnce"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", v.Name, v.MountPath, v.Size, className, mode)
	}
	_ = w.Flush()

	return nil
}

func runVolumesSet(cfg *config.Config, serviceName, specFile, volumesFile string) error {
	if volumesFile == "" {
		return fmt.Errorf("--volumes-file is required")
	}

	data, err := os.ReadFile(volumesFile)
	if err != nil {
		return fmt.Errorf("read volumes file: %w", err)
	}

	var volumes []serviceVolume
	if err := json.Unmarshal(data, &volumes); err != nil {
		return fmt.Errorf("parse volumes JSON: %w", err)
	}

	return patchServiceVolumes(cfg, serviceName, specFile, volumes)
}

func runVolumesAdd(cfg *config.Config, serviceName, specFile, name, mountPath, size, storageClass, accessMode string) error {
	name = strings.TrimSpace(name)
	mountPath = strings.TrimSpace(mountPath)
	if name == "" || mountPath == "" {
		return fmt.Errorf("--name and --mount-path are required")
	}

	ctx := context.Background()
	service, _, err := resolveService(ctx, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	volumes, err := fetchServiceVolumes(ctx, cfg, service.ID.String())
	if err != nil {
		return err
	}

	newVol := serviceVolume{
		Name:             name,
		MountPath:        mountPath,
		Size:             size,
		StorageClassName: storageClass,
		AccessMode:       accessMode,
	}

	replaced := false
	for i, v := range volumes {
		if v.Name == name {
			volumes[i] = newVol
			replaced = true
			break
		}
	}
	if !replaced {
		volumes = append(volumes, newVol)
	}

	return patchServiceVolumes(cfg, serviceName, specFile, volumes)
}

func runVolumesClear(cfg *config.Config, serviceName, specFile string) error {
	return patchServiceVolumes(cfg, serviceName, specFile, []serviceVolume{})
}

func fetchServiceVolumes(ctx context.Context, cfg *config.Config, serviceID string) ([]serviceVolume, error) {
	var resp serviceSettingsResponse
	path := fmt.Sprintf("/v1/services/%s/settings", serviceID)
	if err := apiRequest(ctx, cfg, "GET", path, nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to load volumes: %w", err)
	}
	return resp.Settings.Volumes, nil
}

func patchServiceVolumes(cfg *config.Config, serviceName, specFile string, volumes []serviceVolume) error {
	ctx := context.Background()
	service, _, err := resolveService(ctx, cfg, serviceName, specFile)
	if err != nil {
		return err
	}

	payload := map[string]any{"volumes": volumes}
	path := fmt.Sprintf("/v1/services/%s", service.ID.String())
	if err := apiRequest(ctx, cfg, "PATCH", path, payload, nil); err != nil {
		return fmt.Errorf("failed to update volumes: %w", err)
	}

	fmt.Printf("✅ Updated %d volume(s) on %s — redeploy to apply PVCs\n", len(volumes), service.Name)
	return nil
}
