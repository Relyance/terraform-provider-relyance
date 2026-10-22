package connection

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/relyance/terraform-provider-relyance/internal/client"
)

func TestDeploymentOptionalComputedAndRefresh(t *testing.T) {
	s := resourceSchema(t)
	a, ok := s.Attributes["inhost_deployment_id"].(schema.StringAttribute)
	if !ok || !a.Optional || !a.Computed {
		t.Fatal("assignment must be Optional+Computed")
	}
	m := resourceModel{InhostDeploymentID: types.StringUnknown()}
	for _, id := range []string{"east", ""} {
		d := &client.ConnectionDetail{Connection: map[string]any{"inhost_deployment_id": id}}
		if diags := m.refreshFromAPI(context.Background(), d); diags.HasError() {
			t.Fatal(diags)
		}
		if id == "" && !m.InhostDeploymentID.IsNull() {
			t.Fatal("absent assignment must be null")
		}
		if id != "" && m.InhostDeploymentID.ValueString() != id {
			t.Fatal("import/read must preserve assignment")
		}
	}
}

func TestDeploymentAndModeAreOneRequestBeforeAuth(t *testing.T) {
	f := &fakeService{detail: &client.ConnectionDetail{Connection: map[string]any{"runtime_mode": "IN_HOST_BYOK", "inhost_deployment_id": "east"}}}
	r := &connectionResource{svc: f}
	p := &resourceModel{Vendor: types.StringValue("github"), ID: types.StringValue("3"), RuntimeMode: types.StringValue("IN_HOST_BYOK"), InhostDeploymentID: types.StringValue("east")}
	var d diag.Diagnostics
	if !r.applyRuntimeMode(context.Background(), p, nil, &d) || d.HasError() {
		t.Fatal(d)
	}
	if len(f.scalarReqs) != 1 || f.scalarReqs[0].RuntimeMode == nil || f.scalarReqs[0].InhostDeploymentID == nil || *f.scalarReqs[0].InhostDeploymentID != "east" {
		t.Fatalf("atomic request: %+v", f.scalarReqs)
	}
	// A repeated unchanged apply must not send a route update.
	if !r.applyRuntimeMode(context.Background(), p, p, &d) || len(f.scalarReqs) != 1 {
		t.Fatal("unchanged binding emitted a PATCH")
	}
}

func TestDeploymentOmittedAndUnknownDoNotClear(t *testing.T) {
	for _, id := range []types.String{types.StringNull(), types.StringUnknown()} {
		f := &fakeService{}
		r := &connectionResource{svc: f}
		p := &resourceModel{RuntimeMode: types.StringNull(), InhostDeploymentID: id}
		old := &resourceModel{RuntimeMode: types.StringValue("IN_HOST_BYOK"), InhostDeploymentID: types.StringValue("east")}
		var d diag.Diagnostics
		if !r.applyRuntimeMode(context.Background(), p, old, &d) || len(f.scalarReqs) != 0 {
			t.Fatal("unmanaged/unknown assignment must not mutate")
		}
	}
}

func TestDeploymentUnsupportedServerRefusesBeforeAuth(t *testing.T) {
	f := &fakeService{detail: &client.ConnectionDetail{Connection: map[string]any{"runtime_mode": "IN_HOST_BYOK"}}}
	r := &connectionResource{svc: f}
	p := &resourceModel{RuntimeMode: types.StringValue("IN_HOST_BYOK"), InhostDeploymentID: types.StringValue("east")}
	var d diag.Diagnostics
	if r.applyRuntimeMode(context.Background(), p, nil, &d) || !d.HasError() {
		t.Fatal("server silently ignored assignment")
	}
	if !strings.Contains(d[0].Detail(), "support") {
		t.Fatal(d)
	}
	if len(f.saveReqs) != 0 {
		t.Fatal("auth saved before verified binding")
	}
}
