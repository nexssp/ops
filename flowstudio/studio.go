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
	"sort"
	"strings"
	"time"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
)

const (
	maxSourceBytes  = 2 << 20
	maxBodyBytes    = maxSourceBytes + (64 << 10)
	maxOutputBytes  = 1 << 20
	maxProjectFiles = 128
	maxExplainBytes = 4 << 20
	flowTimeout     = 2 * time.Minute
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
	rootDir  string
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
	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return nil, fmt.Errorf("open Flow file %s: %w", absPath, err)
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
		rootDir:  filepath.Dir(absPath),
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
		s.getProject(w, r)
	case "file":
		if r.Method != http.MethodGet {
			methodNotImplemented(w, "GET", r.Method)
			return
		}
		s.getFile(w, r)
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
	case "explain":
		if r.Method != http.MethodGet {
			methodNotImplemented(w, "GET", r.Method)
			return
		}
		s.explainProject(w, r)
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

type studioFile struct {
	Path      string       `json:"path"`
	Name      string       `json:"name"`
	Namespace string       `json:"namespace,omitempty"`
	Children  []studioFile `json:"children,omitempty"`
}

type projectSnapshot struct {
	Source     string
	Tree       studioFile
	Files      map[string]string
	IndexError string
}

func (s *Studio) projectSnapshot() (projectSnapshot, error) {
	source, err := os.ReadFile(s.filePath)
	if err != nil {
		return projectSnapshot{}, fmt.Errorf("could not read %s: %w", s.fileName, err)
	}
	if len(source) > maxSourceBytes {
		return projectSnapshot{}, fmt.Errorf("%s is larger than %d bytes", s.fileName, maxSourceBytes)
	}
	snapshot := projectSnapshot{
		Source: string(source),
		Tree:   studioFile{Path: s.fileName, Name: s.fileName},
		Files:  map[string]string{s.fileName: s.filePath},
	}
	ctx := compiler.WithSourceLoader(context.Background(), projectSourceLoader{rootDir: s.rootDir})
	index, _, indexErr := compiler.IndexSourceWithMetadata(ctx, native.Directives(), snapshot.Source, s.filePath)
	if indexErr != nil {
		snapshot.IndexError = indexErr.Error()
	}
	ancestors := map[string]bool{s.filePath: true}
	includeCount := 0
	if err := s.appendImports(&snapshot.Tree, index.Imports, snapshot.Files, ancestors, &includeCount); err != nil && snapshot.IndexError == "" {
		snapshot.IndexError = err.Error()
	}
	return snapshot, nil
}

func (s *Studio) appendImports(parent *studioFile, imports []compiler.SourceImport, files map[string]string, ancestors map[string]bool, includeCount *int) error {
	for _, imported := range imports {
		if imported.Source == nil {
			continue
		}
		*includeCount++
		if *includeCount > maxProjectFiles {
			return fmt.Errorf("Flow project has more than %d @include references", maxProjectFiles)
		}
		name, _ := imported.Source.SourceIdentity()
		filePath, relative, err := s.resolveIncludedPath(name)
		if err != nil {
			return fmt.Errorf("index included Flow file: %w", err)
		}
		files[relative] = filePath
		child := studioFile{Path: relative, Name: filepath.Base(relative), Namespace: imported.Namespace}
		parent.Children = append(parent.Children, child)
		if ancestors[filePath] {
			continue
		}
		ancestors[filePath] = true
		if err := s.appendImports(&parent.Children[len(parent.Children)-1], imported.Source.SourceIndex().Imports, files, ancestors, includeCount); err != nil {
			return err
		}
		delete(ancestors, filePath)
	}
	return nil
}

func (s *Studio) resolveIncludedPath(name string) (absolute, relative string, err error) {
	if name == "" {
		return "", "", errors.New("Flow include has no resolved source path")
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(s.rootDir, resolved)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("included file %q is outside the Flow Studio project", name)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() || filepath.Ext(resolved) != ".nflow" {
		return "", "", fmt.Errorf("included file %q must be a regular .nflow file", name)
	}
	if info.Size() > maxSourceBytes {
		return "", "", fmt.Errorf("included file %q is larger than %d bytes", name, maxSourceBytes)
	}
	return resolved, filepath.ToSlash(rel), nil
}

func (s *Studio) getProject(w http.ResponseWriter, _ *http.Request) {
	snapshot, err := s.projectSnapshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"file": s.fileName, "source": snapshot.Source, "tree": snapshot.Tree,
		"index_error": snapshot.IndexError,
	})
}

func (s *Studio) getFile(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.projectSnapshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "")
		return
	}
	name, err := normalizeProjectPath(r.URL.Query().Get("path"), s.fileName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	filePath, ok := snapshot.Files[name]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("%q is not in the Flow include tree.", name), "")
		return
	}
	source, err := os.ReadFile(filePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Could not read %s: %v", name, err), "")
		return
	}
	if len(source) > maxSourceBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("%s is larger than %d bytes.", name, maxSourceBytes), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"file": name, "source": string(source)})
}

func normalizeProjectPath(requested, root string) (string, error) {
	if requested == "" {
		return root, nil
	}
	if filepath.IsAbs(requested) {
		return "", errors.New("Studio file paths must be relative to the entrypoint")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(requested)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("Studio file path escapes the Flow project")
	}
	if filepath.Ext(clean) != ".nflow" {
		return "", errors.New("Studio only opens .nflow files")
	}
	return clean, nil
}

func (s *Studio) saveProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path   string `json:"path"`
		Source string `json:"source"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Source) > maxSourceBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Flow source is larger than %d bytes.", maxSourceBytes), "")
		return
	}
	snapshot, err := s.projectSnapshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "")
		return
	}
	name, err := normalizeProjectPath(request.Path, s.fileName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	filePath, ok := snapshot.Files[name]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Sprintf("%q is not in the Flow include tree.", name), "")
		return
	}
	if err := atomicWriteProjectFile(s.rootDir, filePath, []byte(request.Source)); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Could not save %s: %v", name, err), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Saved."})
}

func atomicWriteProjectFile(rootDir, filePath string, source []byte) error {
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootDir, resolved)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("file is outside the Flow Studio project")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("Flow file is not a regular file")
	}
	tmp, err := os.CreateTemp(filepath.Dir(resolved), ".flowstudio-save-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(source); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, resolved)
}

type projectSourceLoader struct {
	rootDir string
}

func (l projectSourceLoader) Load(currentFile, target string) ([]byte, string, error) {
	if target == "" || strings.Contains(target, ":") {
		return nil, "", fmt.Errorf("Studio supports only local @include files: %q", target)
	}
	requested := filepath.FromSlash(target)
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(filepath.Dir(currentFile), requested)
	}
	absPath, err := filepath.Abs(requested)
	if err != nil {
		return nil, "", err
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return nil, "", err
	}
	rel, err := filepath.Rel(l.rootDir, resolved)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("included file %q is outside the Flow Studio project", target)
	}
	if filepath.Ext(resolved) != ".nflow" {
		return nil, "", fmt.Errorf("included file %q is not a .nflow file", target)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("included file %q is not a regular file", target)
	}
	if info.Size() > maxSourceBytes {
		return nil, "", fmt.Errorf("included file %q is larger than %d bytes", target, maxSourceBytes)
	}
	data, err := os.ReadFile(resolved)
	if err == nil && len(data) > maxSourceBytes {
		return nil, "", fmt.Errorf("included file %q is larger than %d bytes", target, maxSourceBytes)
	}
	return data, resolved, err
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

type modulePlan struct {
	File string          `json:"file"`
	Plan json.RawMessage `json:"plan"`
}

func (s *Studio) explainProject(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.projectSnapshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "")
		return
	}
	if snapshot.IndexError != "" {
		writeError(w, http.StatusUnprocessableEntity, "Flow include index failed: "+snapshot.IndexError, "")
		return
	}
	files := make([]string, 0, len(snapshot.Files))
	for name := range snapshot.Files {
		files = append(files, name)
	}
	sort.Strings(files)
	ctx, cancel := context.WithTimeout(r.Context(), flowTimeout)
	defer cancel()
	plans := make([]modulePlan, 0, len(files))
	responseBytes := 0
	for _, name := range files {
		result, err := s.runFlow(ctx, []string{"flow", "explain", "--plan", "--json", snapshot.Files[name]})
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("Flow Explain failed for %s: %v", name, err), result.Stdout)
			return
		}
		responseBytes += len(result.Stdout)
		if responseBytes > maxExplainBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "Flow Explain output is too large for Studio.", "")
			return
		}
		plan := json.RawMessage(strings.TrimSpace(result.Stdout))
		if !json.Valid(plan) {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("Flow Explain returned invalid plan JSON for %s.", name), result.Stdout)
			return
		}
		plans = append(plans, modulePlan{File: name, Plan: plan})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entrypoint": s.fileName,
		"map":        snapshot.Tree,
		"modules":    plans,
	})
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
