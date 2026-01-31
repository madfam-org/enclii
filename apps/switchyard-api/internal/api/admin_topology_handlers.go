package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetAdminTopology returns topology data for the admin control plane
func (h *Handler) GetAdminTopology(c *gin.Context) {
	ctx := c.Request.Context()

	// Gather all data sources
	clusters, _ := h.repos.Clusters.List(ctx)
	hosts, _ := h.repos.BareMetalHosts.List(ctx)
	vclusters, _ := h.repos.VirtualClusters.List(ctx)

	type TopologyNode struct {
		ID       string                 `json:"id"`
		Type     string                 `json:"type"`
		Label    string                 `json:"label"`
		Status   string                 `json:"status"`
		Data     map[string]interface{} `json:"data,omitempty"`
		Position map[string]float64     `json:"position"`
	}

	type TopologyEdge struct {
		ID     string `json:"id"`
		Source string `json:"source"`
		Target string `json:"target"`
		Label  string `json:"label,omitempty"`
	}

	var nodes []TopologyNode
	var edges []TopologyEdge

	// Add cluster nodes
	for i, c := range clusters {
		nodes = append(nodes, TopologyNode{
			ID: c.ID.String(), Type: "cluster", Label: c.Name, Status: string(c.Status),
			Position: map[string]float64{"x": float64(i * 300), "y": 0},
		})
	}

	// Add BMH nodes and edges to clusters
	for i, h := range hosts {
		nodes = append(nodes, TopologyNode{
			ID: h.ID.String(), Type: "bmh", Label: h.Name, Status: string(h.State),
			Position: map[string]float64{"x": float64(i * 200), "y": 200},
		})
		if h.ClusterID != nil {
			edges = append(edges, TopologyEdge{
				ID: "bmh-" + h.ID.String(), Source: h.ID.String(), Target: h.ClusterID.String(), Label: "member",
			})
		}
	}

	// Add vCluster nodes and edges
	for i, vc := range vclusters {
		nodes = append(nodes, TopologyNode{
			ID: vc.ID.String(), Type: "vcluster", Label: vc.Name, Status: string(vc.Status),
			Position: map[string]float64{"x": float64(i * 250), "y": 400},
		})
		edges = append(edges, TopologyEdge{
			ID: "vc-" + vc.ID.String(), Source: vc.ID.String(), Target: vc.HostClusterID.String(), Label: "hosted-on",
		})
	}

	c.JSON(http.StatusOK, gin.H{"nodes": nodes, "edges": edges})
}
