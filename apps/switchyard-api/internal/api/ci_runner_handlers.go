package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// UpdateCIRunnerConfigRequest represents a request to change CI runner mode
type UpdateCIRunnerConfigRequest struct {
	Mode string `json:"mode" binding:"required"`
}

// CIRunnerConfigResponse represents the CI runner configuration for a project
type CIRunnerConfigResponse struct {
	Mode    string `json:"mode"`
	Message string `json:"message,omitempty"`
}

// GetCIRunnerConfig returns the current CI runner mode for a project.
// GET /v1/projects/:slug/ci-runner-config
func (h *Handler) GetCIRunnerConfig(c *gin.Context) {
	slug := c.Param("slug")

	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		return
	}

	c.JSON(http.StatusOK, CIRunnerConfigResponse{
		Mode: string(project.CIRunnerMode),
	})
}

// UpdateCIRunnerConfig changes the CI runner mode for a project.
// When switching to self-hosted, it sets ARC_BOOTSTRAP_COMPLETE=true on the
// GitHub repo. When switching to github, it sets the variable to false.
// PUT /v1/projects/:slug/ci-runner-config
func (h *Handler) UpdateCIRunnerConfig(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	var req UpdateCIRunnerConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	mode := types.CIRunnerMode(req.Mode)
	if mode != types.CIRunnerModeGitHub && mode != types.CIRunnerModeSelfHosted {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mode must be 'github' or 'self-hosted'"})
		return
	}

	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		return
	}

	// Update the database
	if err := h.repos.Projects.UpdateCIRunnerMode(ctx, project.ID, mode); err != nil {
		h.logger.Error(ctx, "Failed to update CI runner mode",
			logging.String("project", slug),
			logging.Error("error", err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update CI runner mode"})
		return
	}

	// Try to propagate to GitHub via user's OAuth token
	message := ""
	idpToken := c.GetHeader("X-IDP-Token")
	if idpToken == "" {
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			idpToken = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if idpToken != "" {
		tokenResp, err := h.getJanuaToken(ctx, "github", idpToken)
		if err == nil && tokenResp != nil {
			// Find the GitHub repo for this project's services
			services, _ := h.repos.Services.ListByProjectID(project.ID)
			for _, svc := range services {
				if svc.GitRepo == "" {
					continue
				}
				parts := strings.SplitN(svc.GitRepo, "/", 2)
				if len(parts) != 2 {
					continue
				}
				owner, repo := parts[0], parts[1]

				varValue := "false"
				if mode == types.CIRunnerModeSelfHosted {
					varValue = "true"
				}

				// Set the GitHub Actions variable using user's token
				if err := setGitHubVariable(ctx, tokenResp.AccessToken, owner, repo, "ARC_BOOTSTRAP_COMPLETE", varValue); err != nil {
					h.logger.Warn(ctx, "Failed to set GitHub variable (user may lack permissions)",
						logging.String("repo", svc.GitRepo),
						logging.Error("error", err),
					)
					message = "CI runner mode updated in Enclii. GitHub variable could not be set automatically — you may need to set ARC_BOOTSTRAP_COMPLETE manually."
				}
			}
		}
	}

	resp := CIRunnerConfigResponse{Mode: string(mode)}
	if message != "" {
		resp.Message = message
	}
	c.JSON(http.StatusOK, resp)
}

// setGitHubVariable creates or updates a GitHub Actions variable on a repository
// using a user access token.
func setGitHubVariable(ctx context.Context, token, owner, repo, name, value string) error {
	// Implementation uses the same HTTP pattern as the GitHub App client methods
	// but with a user OAuth token instead
	payload := []byte(`{"name":"` + name + `","value":"` + value + `"}`)

	// Try PATCH first (update existing variable)
	url := "https://api.github.com/repos/" + owner + "/" + repo + "/actions/variables/" + name
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	// Variable doesn't exist — create it
	if resp.StatusCode == http.StatusNotFound {
		createURL := "https://api.github.com/repos/" + owner + "/" + repo + "/actions/variables"
		req, err = http.NewRequestWithContext(ctx, "POST", createURL, strings.NewReader(string(payload)))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("Content-Type", "application/json")

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusCreated {
			return nil
		}
	}

	return fmt.Errorf("GitHub API error: %d", resp.StatusCode)
}
