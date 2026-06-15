package types_test

import (
	"strings"
	"testing"

	"dr/server/types"
)

func strPtr(s string) *string { return &s }

func TestReportType_Validate(t *testing.T) {
	if !types.ReportTypeUser.Validate() || !types.ReportTypeAI.Validate() {
		t.Fatal("user and ai must be valid report types")
	}
	if types.ReportType("group").Validate() {
		t.Fatal("unknown report type must be invalid")
	}
}

func TestValidReason(t *testing.T) {
	cases := []struct {
		typ    types.ReportType
		reason string
		want   bool
	}{
		{types.ReportTypeUser, "harassment", true},
		{types.ReportTypeUser, "other", true},
		{types.ReportTypeUser, "unsafe", false}, // ai-only reason
		{types.ReportTypeAI, "medical_mental_health", true},
		{types.ReportTypeAI, "legal_financial", true},
		{types.ReportTypeAI, "harassment", false}, // user-only reason
		{types.ReportTypeAI, "", false},
	}
	for _, c := range cases {
		if got := types.ValidReason(c.typ, c.reason); got != c.want {
			t.Errorf("ValidReason(%s, %q) = %v, want %v", c.typ, c.reason, got, c.want)
		}
	}
}

func TestCreateReportRequest_Validate(t *testing.T) {
	longNote := strings.Repeat("x", 2001)

	cases := []struct {
		name    string
		req     types.CreateReportRequest
		wantErr bool
	}{
		{
			name: "valid user report",
			req: types.CreateReportRequest{
				Type:           types.ReportTypeUser,
				Reason:         "harassment",
				ReportedUserID: strPtr("11111111-1111-1111-1111-111111111111"),
			},
		},
		{
			name: "user report missing reported_user_id",
			req: types.CreateReportRequest{
				Type:   types.ReportTypeUser,
				Reason: "harassment",
			},
			wantErr: true,
		},
		{
			name: "user report with ai reason",
			req: types.CreateReportRequest{
				Type:           types.ReportTypeUser,
				Reason:         "unsafe",
				ReportedUserID: strPtr("11111111-1111-1111-1111-111111111111"),
			},
			wantErr: true,
		},
		{
			name: "valid ai report with output_id",
			req: types.CreateReportRequest{
				Type:       types.ReportTypeAI,
				Reason:     "unsafe",
				OutputType: strPtr("roadmap"),
				OutputID:   strPtr("22222222-2222-2222-2222-222222222222"),
			},
		},
		{
			name: "valid ai report with context only (unsaved preview)",
			req: types.CreateReportRequest{
				Type:       types.ReportTypeAI,
				Reason:     "inaccurate",
				OutputType: strPtr("roadmap_revision"),
				Context:    map[string]any{"text": "the AI suggested something off"},
			},
		},
		{
			name: "ai report missing output_type",
			req: types.CreateReportRequest{
				Type:    types.ReportTypeAI,
				Reason:  "unsafe",
				Context: map[string]any{"text": "x"},
			},
			wantErr: true,
		},
		{
			name: "ai report with neither output_id nor context",
			req: types.CreateReportRequest{
				Type:       types.ReportTypeAI,
				Reason:     "unsafe",
				OutputType: strPtr("task"),
			},
			wantErr: true,
		},
		{
			name: "missing reason",
			req: types.CreateReportRequest{
				Type:           types.ReportTypeUser,
				ReportedUserID: strPtr("11111111-1111-1111-1111-111111111111"),
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			req: types.CreateReportRequest{
				Type:   types.ReportType("spam-bots"),
				Reason: "other",
			},
			wantErr: true,
		},
		{
			name: "note too long",
			req: types.CreateReportRequest{
				Type:           types.ReportTypeUser,
				Reason:         "spam",
				ReportedUserID: strPtr("11111111-1111-1111-1111-111111111111"),
				Note:           &longNote,
			},
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}
