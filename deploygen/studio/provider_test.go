package studio

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/nexssp/kernel/action"
)

type mutableActionProvider struct {
	mu      sync.RWMutex
	actions []action.AnyAction
}

func (p *mutableActionProvider) Actions() []action.AnyAction {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]action.AnyAction(nil), p.actions...)
}

func (p *mutableActionProvider) setActions(actions ...action.AnyAction) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.actions = append([]action.AnyAction(nil), actions...)
}

type providerTestRequest struct {
	Name string `json:"name" validate:"required"`
}

type providerTestResponse struct {
	Greeting string `json:"greeting"`
}

func providerTestAction(name string) action.AnyAction {
	return action.New(name, func(_ context.Context, req providerTestRequest) (providerTestResponse, error) {
		return providerTestResponse{Greeting: "hello " + req.Name}, nil
	}).Build()
}

func TestServiceRefreshesLiveActionProviderAtRequestTime(t *testing.T) {
	provider := &mutableActionProvider{}
	service := NewService(nil)
	service.SetActionProvider(provider)

	catalog, err := service.handleActions(context.Background(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 0 {
		t.Fatalf("initial catalog has %d actions, want 0", len(catalog))
	}

	first := providerTestAction("demo.first")
	provider.setActions(first)
	catalog, err = service.handleActions(context.Background(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 1 || catalog[0].Name != "demo.first" {
		t.Fatalf("catalog after provider update = %+v, want demo.first", catalog)
	}

	firstExecution, err := service.handleExecute(context.Background(), ExecuteReq{
		ActionName: "demo.first",
		Payload:    json.RawMessage(`{"name":"Ada"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	firstResponse, ok := firstExecution.Response.(providerTestResponse)
	if !firstExecution.Success || !ok || firstResponse.Greeting != "hello Ada" {
		t.Fatalf("execution from live provider = %+v", firstExecution)
	}

	second := providerTestAction("demo.second")
	provider.setActions(second)
	if _, err := service.handleExecute(context.Background(), ExecuteReq{ActionName: "demo.first"}); err == nil {
		t.Fatal("removed provider action remained executable")
	}
	secondExecution, err := service.handleExecute(context.Background(), ExecuteReq{ActionName: "demo.second"})
	if err != nil {
		t.Fatal(err)
	}
	if !secondExecution.Success {
		t.Fatalf("new provider action did not execute: %+v", secondExecution)
	}

	scenario, err := service.handleFuzz(context.Background(), struct {
		ActionName string `json:"action_name" validate:"required"`
		Scenario   string `json:"scenario" validate:"required"`
	}{ActionName: "demo.second", Scenario: "happy"})
	if err != nil {
		t.Fatal(err)
	}
	if scenario["action_name"] != "demo.second" {
		t.Fatalf("scenario did not resolve the live provider action: %+v", scenario)
	}

	universe, err := service.handleSecurity(context.Background(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := universe.Actions["demo.second"]; !ok {
		t.Fatalf("security catalog did not refresh from provider: %+v", universe.Actions)
	}
}

func TestNewServiceRetainsStaticCatalogSnapshot(t *testing.T) {
	original := providerTestAction("demo.original")
	replacement := providerTestAction("demo.replacement")
	actions := []action.AnyAction{original}
	service := NewService(actions)
	actions[0] = replacement

	catalog, err := service.handleActions(context.Background(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 1 || catalog[0].Name != "demo.original" {
		t.Fatalf("static catalog changed after input slice mutation: %+v", catalog)
	}

	if _, err := service.handleExecute(context.Background(), ExecuteReq{ActionName: "demo.original"}); err != nil {
		t.Fatalf("original static action was not executable: %v", err)
	}
	if _, err := service.handleExecute(context.Background(), ExecuteReq{ActionName: "demo.replacement"}); err == nil {
		t.Fatal("static service picked up an action added after construction")
	}
}
