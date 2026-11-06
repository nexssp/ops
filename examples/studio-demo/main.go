package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"
	"github.com/nexssp/transport/thttp"

	"github.com/nexssp/ops/analyzer"
	"github.com/nexssp/ops/deploygen/studio"
)

type EchoRequest struct {
	Message string `json:"message" validate:"required"`
	Repeat  int    `json:"repeat" validate:"min=1,max=5"`
}

type EchoResponse struct {
	Message     string `json:"message"`
	Repetitions int    `json:"repetitions"`
	GeneratedAt string `json:"generated_at"`
}

type CalculateRequest struct {
	Left     float64 `json:"left"`
	Right    float64 `json:"right"`
	Operator string  `json:"operator" validate:"required"`
}

type CalculateResponse struct {
	Left     float64 `json:"left"`
	Right    float64 `json:"right"`
	Operator string  `json:"operator"`
	Result   float64 `json:"result"`
}

type ValidateRequest struct {
	Title string `json:"title" validate:"required"`
}

type ValidateResponse struct {
	Accepted bool   `json:"accepted"`
	Title    string `json:"title"`
}

type SecurePreviewRequest struct {
	DocumentID string `json:"document_id" validate:"required"`
}

type SecurePreviewResponse struct {
	DocumentID string `json:"document_id"`
	Preview    string `json:"preview"`
}

func demoActions() []action.AnyAction {
	echo := action.New("demo.echo", func(_ context.Context, req EchoRequest) (EchoResponse, error) {
		return EchoResponse{
			Message:     req.Message,
			Repetitions: req.Repeat,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}, nil
	}).Description("Echo a message and return a timestamped response.").Tag("demo", "utility").
		Idempotent().Timeout(2 * time.Second).Route(thttp.POST("/api/demo/echo")).Build()

	calculate := action.New("demo.calculate", func(_ context.Context, req CalculateRequest) (CalculateResponse, error) {
		var result float64
		switch req.Operator {
		case "+":
			result = req.Left + req.Right
		case "-":
			result = req.Left - req.Right
		case "*":
			result = req.Left * req.Right
		case "/":
			if req.Right == 0 {
				return CalculateResponse{}, xerr.BadRequest("cannot divide by zero")
			}
			result = req.Left / req.Right
		default:
			return CalculateResponse{}, xerr.Validation("operator must be one of +, -, *, or /")
		}
		return CalculateResponse{Left: req.Left, Right: req.Right, Operator: req.Operator, Result: result}, nil
	}).Description("Calculate a basic arithmetic result.").Tag("demo", "utility").
		Timeout(2 * time.Second).Route(thttp.POST("/api/demo/calculate")).Build()

	validate := action.New("demo.validate", func(_ context.Context, req ValidateRequest) (ValidateResponse, error) {
		if req.Title == "reject" {
			return ValidateResponse{}, xerr.Validation("the demo policy rejects this title")
		}
		return ValidateResponse{Accepted: true, Title: req.Title}, nil
	}).Description("Show a successful response or a structured validation error.").Tag("demo", "validation").
		Timeout(2 * time.Second).Route(thttp.POST("/api/demo/validate")).Build()

	secure := action.New("demo.secure-preview", func(_ context.Context, req SecurePreviewRequest) (SecurePreviewResponse, error) {
		return SecurePreviewResponse{DocumentID: req.DocumentID, Preview: "Preview access granted for " + req.DocumentID}, nil
	}).Description("Preview a document guarded by a role, permission, and feature claim.").
		Tag("demo", "security").RequireRole("operator").RequirePermission("demo.write").
		RequireFeature("preview-v2").Route(thttp.POST("/api/demo/secure-preview")).Build()

	return []action.AnyAction{echo, calculate, validate, secure}
}

func demoRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "examples/studio-demo"
	}
	return filepath.Dir(filename)
}

func demoFlowFile() string { return filepath.Join(demoRoot(), "app.nflow") }

func demoReport() (*analyzer.Report, error) { return analyzer.Analyze(demoRoot()) }

func mustDemoReport() *analyzer.Report {
	report, err := demoReport()
	if err != nil {
		panic(fmt.Errorf("analyze Studio demo routes: %w", err))
	}
	return report
}

func demoRuntime() (*studio.Service, []compiler.Bundle) {
	business := demoActions()
	service := studio.NewService(business, mustDemoReport())
	bundles := []compiler.Bundle{{
		ID: "nexssp-studio-demo",
		Libraries: []action.Library{
			{Name: "demo", Actions: business},
			{Name: "studio", Actions: service.Actions()},
		},
	}}
	return service, bundles
}

func demoBundles() []compiler.Bundle {
	_, bundles := demoRuntime()
	return bundles
}

// newHandler supports focused HTTP tests of the Studio and registered Go actions.
// TestLiveNFlowRoute uses the generated Flow backend to verify the NFlow route itself.
func newHandler() http.Handler {
	business := demoActions()
	studioService := studio.NewService(business, mustDemoReport())
	all := append(studioService.Actions(), business...)
	transport := thttp.New("")
	transport.Mount(all)
	return transport.Handler()
}

func main() {
	const address = "127.0.0.1:8088"
	log.Printf("Nexss Studio demo backend listening at http://%s/studio/", address)
	log.Printf("Go endpoints: POST /api/demo/echo, /api/demo/calculate, /api/demo/validate, /api/demo/secure-preview")
	log.Printf("NFlow endpoint: POST /api/demo/nflow-echo (declared in app.nflow)")
	log.Printf("For identity simulation in this local demo: APP_ENV=development NEXSS_STUDIO_ALLOW_IDENTITY_OVERRIDE=true")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	service, bundles := demoRuntime()
	configureMux := func(mux *http.ServeMux) { mux.Handle("/", service.Handler()) }
	if err := RunStudioDemoBackend(ctx, demoFlowFile(), address, bundles, configureMux); err != nil {
		log.Fatal(fmt.Errorf("Studio demo server: %w", err))
	}
}
