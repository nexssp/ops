package analyzer

import "testing"

func TestExcludeFlowPipelinesRemovesRouteActionAndUpdatesStats(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{
			{Name: "chaos.retry_probe", Scope: "public", RouteSource: "nflow"},
			{Name: "catalog.list", Scope: "public", RouteSource: "nflow"},
			{Name: "catalog.create", Scope: "public", RouteSource: "nflow"},
			{Name: "worker.cycle", Scope: "internal"},
		},
		FlowHTTPRoutes: []FlowHTTPRoute{
			{Pipeline: "pipeline.chaos_retry_probe"},
			{Pipeline: "pipeline.catalog_list"},
		},
		Stats: ReportStats{TotalActions: 4, PublicActions: 3, InternalActions: 1},
	}

	ExcludeFlowPipelines(report, []string{"pipeline.chaos_retry_probe"})
	if len(report.Actions) != 3 || report.Actions[0].Name != "catalog.list" {
		t.Fatalf("unexpected filtered actions: %#v", report.Actions)
	}
	if len(report.FlowHTTPRoutes) != 1 || report.FlowHTTPRoutes[0].Pipeline != "pipeline.catalog_list" {
		t.Fatalf("unexpected filtered routes: %#v", report.FlowHTTPRoutes)
	}
	if report.Stats.TotalActions != 3 || report.Stats.PublicActions != 2 || report.Stats.InternalActions != 1 {
		t.Fatalf("unexpected action counts after exclusion: %#v", report.Stats)
	}
}
