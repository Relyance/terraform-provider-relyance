// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
)

// BusinessNode is the subset of a business node the provider exposes.
type BusinessNode struct {
	ID   string `json:"record_id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ListBusinessNodes returns all business nodes for the tenant, following
// pagination to completion. Iterations are hard-capped independently of the
// server-reported total_count: a buggy (or hostile) backend that keeps serving
// full pages against an inconsistent count must not grow memory or hang the
// read unboundedly.
func (c *Client) ListBusinessNodes(ctx context.Context) ([]BusinessNode, error) {
	const pageSize = 1000
	const maxPages = 100 // 100k nodes — far beyond any real tenant
	var out []BusinessNode
	for pageNum := int32(1); ; pageNum++ {
		if pageNum > maxPages {
			return nil, fmt.Errorf("business-nodes pagination exceeded %d pages without converging on the server-reported total; aborting", maxPages)
		}
		page, resp, err := c.api.TerraformAPI.BusinessNodesV1(ctx).
			PageSize(pageSize).PageNum(pageNum).Execute()
		if err != nil {
			return nil, apiErr(resp, err)
		}
		for i := range page.Results {
			n := &page.Results[i]
			out = append(out, BusinessNode{ID: n.RecordId, Name: n.Name, Type: n.Type})
		}
		if len(out) >= int(page.TotalCount) || len(page.Results) == 0 {
			return out, nil
		}
	}
}
