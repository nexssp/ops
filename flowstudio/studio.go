package flowstudio

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxSourceBytes = 2 << 20
	maxBodyBytes   = maxSourceBytes + (64 << 10)
	maxOutputBytes = 1 << 20
	flowTimeout    = 2 * time.Minute
)

//go:embed web/dist/*
var webAssets embed.FS

type commandOutput struct {
	Stdout string `json:"output"`
	Stderr string `json:"-"`
}

type commandRunner func(context.Context, []string) (commandOutput, error)

type Studio struct {
	filePath string
	fileName string
	token    string
	run      commandRunner
}

// New creates a Studio for one existing .nflow file. It intentionally does not
// discover or expose arbitrary files; the selected Flow file remains the source
// of truth for editing, validation, and execution.
func New(filePath string) (*Studio, error) {
	return newStudio(filePath, runNexsspFlow)
}

func newStudio(filePath string, run commandRunner) (*Studio, error) {
	if run == nil {
		return nil, errors.New("flow studio: Flow command runner is required")
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve Flow file: %w", err)
	}
	if filepath.Ext(absPath) != ".nflow" {
		return nil, fmt.Errorf("Flow Studio only opens .nflow files: %s", filepath.Base(absPath))
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("open Flow file %s: %w", absPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Flow file is not a regular file: %s", absPath)
	}
	if info.Size() > maxSourceBytes {
		return nil, fmt.Errorf("Flow file is larger than %d bytes", maxSourceBytes)
	}
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return nil, fmt.Errorf("create local Studio token: %w", err)
	}
	return &Studio{
		filePath: absPath,
		fileName: filepath.Base(absPath),
		token:    hex.EncodeToString(tokenBytes[:]),
		run:      run,
	}, nil
}

// Handler returns the local Studio UI and its narrowly scoped API. Callers
// should bind it to loopback; Run does so automatically.
func (s *Studio) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.handleAPI)
	mux.HandleFunc("/", s.handleWeb)
	return securityHeaders(mux)
}

// Run serves the Studio on an ephemeral loopback port until the context is
// cancelled. Flow execution uses the same `nexssp flow` CLI path used outside
// Studio, including Flow's native bundle and @require handling.
func Run(ctx context.Context, filePath string, output io.Writer) error {
	studio, err := New(filePath)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start local Flow Studio: %w", err)
	}
	defer listener.Close()
	if output != nil {
		fmt.Fprintf(output, "Flow Studio for %s\n", studio.fileName)
		fmt.Fprintf(output, "Open http://%s/ in your browser.\n", listener.Addr())
		fmt.Fprintln(output, "Run executes the saved flow locally with the current user's permissions. Press Ctrl+C to stop.")
	}

	server := &http.Server{
		Handler:           studio.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err = server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("Flow Studio server: %w", err)
	}
	if ctx.Err() != nil {
		<-shutdownDone
	}
	return nil
}

func (s *Studio) handleWeb(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackHost(r.Host) {
		http.Error(w, "Flow Studio is available only on localhost.", http.StatusForbidden)
		return
	}
	if r.URL.Path == "/" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "Not implemented: the Studio page supports GET only.", "")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "flowstudio",
			Value:    s.token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   int((6 * time.Hour).Seconds()),
		})
		w.Header().Set("Cache-Control", "no-store")
		assets, err := fs.Sub(webAssets, "web/dist")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Flow Studio UI assets are unavailable.", "")
			return
		}
		index, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Flow Studio UI is not built; rebuild the bundled Svelte assets.", "")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(index)
		return
	}
	assets, err := fs.Sub(webAssets, "web/dist")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Flow Studio UI assets are unavailable.", "")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		http.FileServer(http.FS(assets)).ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *Studio) handleAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	write := r.Method != http.MethodGet
	if !s.authorized(r, write) {
		writeError(w, http.StatusForbidden, "Studio access denied; open the local Studio page and try again.", "")
		return
	}
	switch path {
	case "project":
		if r.Method != http.MethodGet {
			methodNotImplemented(w, "GET", r.Method)
			return
		}
		s.getProject(w)
	case "save":
		if r.Method != http.MethodPost {
			methodNotImplemented(w, "POST", r.Method)
			return
		}
		s.saveProject(w, r)
	case "validate":
		if r.Method != http.MethodPost {
			methodNotImplemented(w, "POST", r.Method)
			return
		}
		s.validateProject(w, r)
	case "run":
		if r.Method != http.MethodPost {
			methodNotImplemented(w, "POST", r.Method)
			return
		}
		s.runProject(w, r)
	default:
		writeError(w, http.StatusNotImplemented, fmt.Sprintf("Not implemented: Studio API operation %q.", path), "")
	}
}

func (s *Studio) authorized(r *http.Request, write bool) bool {
	if !isLoopbackHost(r.Host) {
		return false
	}
	cookie, err := r.Cookie("flowstudio")
	if err != nil || cookie.Value != s.token {
		return false
	}
	origin := r.Header.Get("Origin")
	if write && origin == "" {
		return false
	}
	if origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return false
		}
		if parsed.Scheme != "http" || parsed.Host != r.Host {
			return false
		}
	}
	return true
}

func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func (s *Studio) getProject(w http.ResponseWriter) {
	source, err := os.ReadFile(s.filePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Could not read %s: %v", s.fileName, err), "")
		return
	}
	if len(source) > maxSourceBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("%s is larger than %d bytes.", s.fileName, maxSourceBytes), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"file": s.fileName, "source": string(source)})
}

func (s *Studio) saveProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Source string `json:"source"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Source) > maxSourceBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Flow source is larger than %d bytes.", maxSourceBytes), "")
		return
	}
	if err := os.WriteFile(s.filePath, []byte(request.Source), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Could not save %s: %v", s.fileName, err), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Saved."})
}

func (s *Studio) validateProject(w http.ResponseWriter, r *http.Request) {
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	result, err := s.runFlow(r.Context(), []string{"flow", s.filePath, "--info", "--json"})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Flow validation failed: "+err.Error(), result.Stdout)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Studio) runProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Payload json.RawMessage `json:"payload"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	args := []string{"flow", s.filePath}
	if len(request.Payload) != 0 && string(request.Payload) != "null" {
		var payload map[string]any
		if err := json.Unmarshal(request.Payload, &payload); err != nil || payload == nil {
			writeError(w, http.StatusBadRequest, "Run payload must be a JSON object.", "")
			return
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Could not encode the run payload.", "")
			return
		}
		args = append(args, string(payloadJSON))
	}
	args = append(args, "--json")
	result, err := s.runFlow(r.Context(), args)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Flow run failed: "+err.Error(), result.Stdout)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Studio) runFlow(parent context.Context, args []string) (commandOutput, error) {
	ctx, cancel := context.WithTimeout(parent, flowTimeout)
	defer cancel()
	result, err := s.run(ctx, args)
	if len(result.Stdout) > maxOutputBytes {
		result.Stdout = result.Stdout[:maxOutputBytes] + "\n[output truncated]"
	}
	if len(result.Stderr) > maxOutputBytes {
		result.Stderr = result.Stderr[:maxOutputBytes] + "\n[error output truncated]"
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("%w", ctx.Err())
		}
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = err.Error()
		}
		return result, errors.New(message)
	}
	return result, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Studio requests must use application/json.", "")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid Studio request: "+err.Error(), "")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "Invalid Studio request: expected one JSON object.", "")
		return false
	}
	return true
}

func methodNotImplemented(w http.ResponseWriter, expected, actual string) {
	w.Header().Set("Allow", expected)
	writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("Not implemented: %s is not supported here; use %s.", actual, expected), "")
}

func writeError(w http.ResponseWriter, status int, message, output string) {
	writeJSON(w, status, map[string]string{"error": message, "output": output})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func runNexsspFlow(ctx context.Context, args []string) (commandOutput, error) {
	executable, err := os.Executable()
	if err != nil {
		return commandOutput{}, fmt.Errorf("locate nexssp executable: %w", err)
	}
	command := exec.CommandContext(ctx, executable, args...)
	var stdout, stderr cappedBuffer
	stdout.limit = maxOutputBytes
	stderr.limit = maxOutputBytes
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.Env = append(os.Environ(), "NO_COLOR=1")
	err = command.Run()
	return commandOutput{Stdout: stdout.String(), Stderr: stderr.String()}, err
}

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.Buffer.Write(p[:remaining])
		} else {
			_, _ = b.Buffer.Write(p)
		}
	}
	return len(p), nil
}
