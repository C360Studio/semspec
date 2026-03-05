package strategies

// build_test.go contains unit tests for all strategy Build() methods.
//
// Testing approach:
//   - filesystem-dependent behaviour: real FileGatherer + GitGatherer on t.TempDir()
//   - graph-dependent paths: httptest.Server acting as the GraphQL endpoint
//   - GraphReady: false short-circuits all graph calls, keeping most tests fast
//   - GraphReady: true + mock HTTP server exercises the graph code paths
//
// No external infrastructure (NATS, real graph-gateway) is required.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semspec/processor/context-builder/gatherers"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestGatherers creates a Gatherers set pointing at repoPath.
// graphURL may be a test-server URL or empty (when GraphReady will be false).
func newTestGatherers(t *testing.T, repoPath, graphURL string) *Gatherers {
	t.Helper()
	if graphURL == "" {
		// Use an address that is syntactically valid but unreachable;
		// strategies guard all graph calls behind req.GraphReady == true.
		graphURL = "http://127.0.0.1:1"
	}
	graph := gatherers.NewGraphGatherer(graphURL)
	return &Gatherers{
		Graph: graph,
		Git:   gatherers.NewGitGatherer(repoPath),
		File:  gatherers.NewFileGatherer(repoPath),
		SOP:   gatherers.NewSOPGatherer(graph, ""),
	}
}

// writeFile creates path (relative to dir) with the given content.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
}

// initGitRepo initialises a bare git repository in dir so GitGatherer can run.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
}

// runGit runs a git command in dir, logging but not fatally failing on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("git %v: %v\n%s", args, err, out)
	}
}

// ---------------------------------------------------------------------------
// mockGraphQL helpers
// ---------------------------------------------------------------------------

// newGraphQLServer starts an httptest.Server that handles POST /graphql.
// Each request consumes the next response from the responses slice.
// After all responses are exhausted the last one is repeated.
// Request bodies are logged so failing tests can show which query got which response.
func newGraphQLServer(t *testing.T, responses []map[string]any) *httptest.Server {
	t.Helper()
	type envelope struct {
		Data map[string]any `json:"data"`
	}
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode request for logging
		var body map[string]any
		bodyBytes := make([]byte, 4096)
		n, _ := r.Body.Read(bodyBytes)
		_ = json.Unmarshal(bodyBytes[:n], &body)
		query, _ := body["query"].(string)
		vars, _ := json.Marshal(body["variables"])

		resp := responses[minInt(idx, len(responses)-1)]
		t.Logf("graphql request %d: query=%q vars=%s → response keys=%v",
			idx, shortenQuery(query), vars, mapKeys(resp))
		idx++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(envelope{Data: resp})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func shortenQuery(q string) string {
	q = strings.TrimSpace(q)
	if len(q) > 80 {
		return q[:80] + "..."
	}
	return q
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// BudgetAllocation unit tests
// ---------------------------------------------------------------------------

func TestBudgetAllocationAllocate(t *testing.T) {
	b := NewBudgetAllocation(1000)

	if err := b.Allocate("item1", 400); err != nil {
		t.Fatalf("Allocate(400): %v", err)
	}
	if b.Allocated != 400 {
		t.Errorf("Allocated = %d, want 400", b.Allocated)
	}
	if b.Remaining() != 600 {
		t.Errorf("Remaining = %d, want 600", b.Remaining())
	}

	// Over-budget should fail.
	if err := b.Allocate("item2", 700); err == nil {
		t.Error("expected error for over-allocation, got nil")
	}

	// Re-allocating the same key should update in place.
	if err := b.Allocate("item1", 200); err != nil {
		t.Fatalf("re-Allocate(200): %v", err)
	}
	if b.Allocated != 200 {
		t.Errorf("after re-allocate Allocated = %d, want 200", b.Allocated)
	}
}

func TestBudgetAllocationTryAllocate(t *testing.T) {
	b := NewBudgetAllocation(500)

	got := b.TryAllocate("a", 300)
	if got != 300 {
		t.Errorf("TryAllocate(300) = %d, want 300", got)
	}

	// Only 200 remaining — should cap.
	got = b.TryAllocate("b", 400)
	if got != 200 {
		t.Errorf("TryAllocate(400) capped = %d, want 200", got)
	}

	// Exhausted — should return 0.
	got = b.TryAllocate("c", 100)
	if got != 0 {
		t.Errorf("TryAllocate on exhausted budget = %d, want 0", got)
	}
}

func TestBudgetAllocationCanFit(t *testing.T) {
	b := NewBudgetAllocation(100)
	_ = b.Allocate("x", 80)

	if !b.CanFit(20) {
		t.Error("CanFit(20) should be true with 20 remaining")
	}
	if b.CanFit(21) {
		t.Error("CanFit(21) should be false with only 20 remaining")
	}
}

// ---------------------------------------------------------------------------
// TokenEstimator unit tests
// ---------------------------------------------------------------------------

func TestTokenEstimatorEstimate(t *testing.T) {
	e := NewTokenEstimator()

	if e.Estimate("") != 0 {
		t.Error("Estimate(\"\") should return 0")
	}
	// 4 chars / 4 chars-per-token = 1 token
	if e.Estimate("abcd") != 1 {
		t.Errorf("Estimate(\"abcd\") = %d, want 1", e.Estimate("abcd"))
	}
}

func TestTokenEstimatorTruncateToTokens(t *testing.T) {
	e := NewTokenEstimator()

	content := strings.Repeat("x", 400) // 400 chars ≈ 100 tokens
	truncated, wasTruncated := e.TruncateToTokens(content, 200)
	if wasTruncated {
		t.Error("TruncateToTokens(≈100 tokens, max=200) should not truncate")
	}
	if truncated != content {
		t.Error("untruncated content should be returned unchanged")
	}

	truncated, wasTruncated = e.TruncateToTokens(content, 50)
	if !wasTruncated {
		t.Error("TruncateToTokens(≈100 tokens, max=50) should truncate")
	}
	suffix := truncated[maxInt(0, len(truncated)-15):]
	if !strings.Contains(suffix, "truncated") {
		t.Errorf("truncated content should end with [truncated]; tail: %q", suffix)
	}
}

// ---------------------------------------------------------------------------
// PlanReviewStrategy.Build() tests
// ---------------------------------------------------------------------------

func TestPlanReviewStrategy_BuildWithPlanContent(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewPlanReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:   "req-pr-1",
		TaskType:    TaskTypePlanReview,
		PlanSlug:    "my-plan",
		PlanContent: `{"tasks":[]}`,
		GraphReady:  false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	planDoc, ok := result.Documents["__plan__"]
	if !ok {
		t.Fatal("expected __plan__ in Documents")
	}
	if !strings.Contains(planDoc, "my-plan") {
		t.Errorf("plan document should reference slug; got:\n%s", planDoc)
	}
	if !strings.Contains(planDoc, `{"tasks":[]}`) {
		t.Errorf("plan document should contain content; got:\n%s", planDoc)
	}
}

func TestPlanReviewStrategy_BuildNoPlanContent(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewPlanReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-pr-2",
		TaskType:   TaskTypePlanReview,
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if _, ok := result.Documents["__plan__"]; ok {
		t.Error("__plan__ should not be present when PlanContent is empty")
	}
}

func TestPlanReviewStrategy_BuildIncludesFileTree(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "go.mod", "module example.com/test\n\ngo 1.21")

	g := newTestGatherers(t, dir, "")
	strategy := NewPlanReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:   "req-pr-3",
		TaskType:    TaskTypePlanReview,
		PlanContent: `{"tasks":[]}`,
		GraphReady:  false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	tree, ok := result.Documents["__file_tree__"]
	if !ok {
		t.Fatal("expected __file_tree__ in Documents")
	}
	if !strings.Contains(tree, "main.go") {
		t.Errorf("file tree should list main.go; got:\n%s", tree)
	}
}

func TestPlanReviewStrategy_BuildGreenfieldDetection(t *testing.T) {
	// Only dotfiles/sources/ in repo — all filtered by PlanReviewStrategy.
	dir := t.TempDir()
	writeFile(t, dir, ".semspec/config.yaml", "key: val")
	writeFile(t, dir, "sources/ingested.md", "# source doc")

	g := newTestGatherers(t, dir, "")
	strategy := NewPlanReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:   "req-pr-4",
		TaskType:    TaskTypePlanReview,
		PlanContent: `{"tasks":[]}`,
		GraphReady:  false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	tree, ok := result.Documents["__file_tree__"]
	if !ok {
		t.Fatal("expected __file_tree__ in Documents")
	}
	if !strings.Contains(tree, "GREENFIELD") {
		t.Errorf("expected GREENFIELD marker; got:\n%s", tree)
	}
}

func TestPlanReviewStrategy_BuildIncludesArchDoc(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "docs/03-architecture.md", "# Architecture\n\nNATS-based microservices.")

	g := newTestGatherers(t, dir, "")
	strategy := NewPlanReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:   "req-pr-5",
		TaskType:    TaskTypePlanReview,
		PlanContent: `{"tasks":[]}`,
		GraphReady:  false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	archDoc, ok := result.Documents["docs/03-architecture.md"]
	if !ok {
		t.Fatal("expected docs/03-architecture.md when present")
	}
	if !strings.Contains(archDoc, "NATS") {
		t.Errorf("arch doc content not included correctly; got:\n%s", archDoc)
	}
}

func TestPlanReviewStrategy_BuildTinyBudgetDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewPlanReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50) // exhausted immediately by plan content

	req := &ContextBuildRequest{
		RequestID:   "req-pr-6",
		TaskType:    TaskTypePlanReview,
		PlanContent: strings.Repeat("x", 2000),
		GraphReady:  false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result even with tiny budget")
	}
	if budget.Allocated > budget.Total {
		t.Errorf("budget over-allocated: %d > %d", budget.Allocated, budget.Total)
	}
}

// ---------------------------------------------------------------------------
// PlanningStrategy.Build() tests
// ---------------------------------------------------------------------------

func TestPlanningStrategy_BuildAmbiguousScopeGeneratesBlockingQuestion(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewPlanningStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	// No Topic, Files, or SpecEntityID.
	req := &ContextBuildRequest{
		RequestID:  "req-pl-1",
		TaskType:   TaskTypePlanning,
		WorkflowID: "wf-999",
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	found := false
	for _, q := range result.Questions {
		if q.Topic == "requirements.scope" && q.Urgency == UrgencyBlocking {
			found = true
		}
	}
	if !found {
		t.Errorf("expected blocking requirements.scope question; got: %+v", result.Questions)
	}
	if !result.InsufficientContext {
		t.Error("InsufficientContext should be true for ambiguous scope")
	}
}

func TestPlanningStrategy_BuildWithTopicAddsFileTree(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "docs/03-architecture.md", "# Arch\n\nDetails.")

	g := newTestGatherers(t, dir, "")
	strategy := NewPlanningStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-pl-2",
		TaskType:   TaskTypePlanning,
		Topic:      "authentication",
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if _, ok := result.Documents["__file_tree__"]; !ok {
		t.Error("expected __file_tree__ in Documents")
	}
	// Arch doc from filesystem fallback.
	if _, ok := result.Documents["docs/03-architecture.md"]; !ok {
		t.Error("expected docs/03-architecture.md from filesystem fallback")
	}
}

func TestPlanningStrategy_BuildRevisionIncludesPlanContent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")

	g := newTestGatherers(t, dir, "")
	strategy := NewPlanningStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:   "req-pl-3",
		TaskType:    TaskTypePlanning,
		Topic:       "auth",
		PlanSlug:    "auth-plan",
		PlanContent: `{"slug":"auth-plan","tasks":[]}`,
		GraphReady:  false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	planDoc, ok := result.Documents["__plan__"]
	if !ok {
		t.Fatal("expected __plan__ for revision context")
	}
	if !strings.Contains(planDoc, "auth-plan") {
		t.Errorf("plan doc should contain slug; got:\n%s", planDoc)
	}
}

func TestPlanningStrategy_BuildWithRequestedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "service.go", "package service\n\n// Handles auth")

	g := newTestGatherers(t, dir, "")
	strategy := NewPlanningStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-pl-4",
		TaskType:   TaskTypePlanning,
		Topic:      "service",
		Files:      []string{"service.go"},
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if _, ok := result.Documents["service.go"]; !ok {
		t.Error("expected service.go in Documents for explicitly requested file")
	}
}

func TestPlanningStrategy_BuildWithGraphEmptyResults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")

	empty := map[string]any{"entitiesByPredicate": []any{}}

	// Serve enough empty responses to cover all graph queries:
	// GetCodebaseSummary (4 queries) + source.doc query (arch docs) +
	// existing specs (semspec.plan + semspec.spec) +
	// code patterns (code.function, code.type, code.interface, code.package)
	responses := make([]map[string]any, 20)
	for i := range responses {
		responses[i] = empty
	}
	srv := newGraphQLServer(t, responses)

	g := newTestGatherers(t, dir, srv.URL)
	strategy := NewPlanningStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-pl-5",
		TaskType:   TaskTypePlanning,
		Topic:      "auth",
		GraphReady: true,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// File tree always present.
	if _, ok := result.Documents["__file_tree__"]; !ok {
		t.Error("expected __file_tree__ even when graph returns empty")
	}
	if budget.Allocated > budget.Total {
		t.Errorf("budget over-allocated: %d > %d", budget.Allocated, budget.Total)
	}
}

func TestPlanningStrategy_BuildBudgetEnforced(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewPlanningStrategy(g, nil)
	budget := NewBudgetAllocation(50) // tiny

	req := &ContextBuildRequest{
		RequestID:   "req-pl-6",
		TaskType:    TaskTypePlanning,
		Topic:       "auth",
		PlanContent: strings.Repeat("x", 5000),
		GraphReady:  false,
	}

	_, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if budget.Allocated > budget.Total {
		t.Errorf("budget over-allocated: %d > %d", budget.Allocated, budget.Total)
	}
}

// ---------------------------------------------------------------------------
// ImplementationStrategy.Build() tests
// ---------------------------------------------------------------------------

func TestImplementationStrategy_BuildNoInputs(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewImplementationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-im-1",
		TaskType:   TaskTypeImplementation,
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if result.Error != "" {
		t.Errorf("unexpected error in result: %s", result.Error)
	}
}

func TestImplementationStrategy_BuildWithSourceFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "handler.go", "package api\n\nfunc Handle() {}")

	g := newTestGatherers(t, dir, "")
	strategy := NewImplementationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-im-2",
		TaskType:   TaskTypeImplementation,
		Files:      []string{"handler.go"},
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if _, ok := result.Documents["handler.go"]; !ok {
		t.Error("expected handler.go in Documents")
	}
}

func TestImplementationStrategy_BuildIncludesArchDoc(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "docs/03-architecture.md", "# Arch\n\nMicroservices.")

	g := newTestGatherers(t, dir, "")
	strategy := NewImplementationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-im-3",
		TaskType:   TaskTypeImplementation,
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if _, ok := result.Documents["docs/03-architecture.md"]; !ok {
		t.Error("expected docs/03-architecture.md when present")
	}
}

func TestImplementationStrategy_BuildSpecEntityGraphNotReady(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewImplementationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:    "req-im-4",
		TaskType:     TaskTypeImplementation,
		SpecEntityID: "semspec.spec.auth",
		GraphReady:   false, // graph not ready → spec fetch skipped
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if _, ok := result.Documents["__spec__"]; ok {
		t.Error("__spec__ should not be present when GraphReady=false")
	}
}

func TestImplementationStrategy_BuildSpecEntityFromGraph(t *testing.T) {
	dir := t.TempDir()

	entityResp := map[string]any{
		"entity": map[string]any{
			"id": "semspec.spec.auth",
			"triples": []any{
				map[string]any{"predicate": "dc.terms.title", "object": "Auth Spec"},
			},
		},
	}
	srv := newGraphQLServer(t, []map[string]any{entityResp})

	g := newTestGatherers(t, dir, srv.URL)
	strategy := NewImplementationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:    "req-im-5",
		TaskType:     TaskTypeImplementation,
		SpecEntityID: "semspec.spec.auth",
		GraphReady:   true,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if result.Error != "" {
		t.Errorf("unexpected result error: %s", result.Error)
	}
	if _, ok := result.Documents["__spec__"]; !ok {
		t.Error("expected __spec__ when graph returns the entity")
	}
	if len(result.Entities) == 0 {
		t.Error("expected EntityRef for the fetched spec")
	}
}

func TestImplementationStrategy_BuildSpecEntityGraphError(t *testing.T) {
	dir := t.TempDir()

	// Serve a GraphQL error response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		type gqlErr struct {
			Message string `json:"message"`
		}
		type envelope struct {
			Errors []gqlErr `json:"errors"`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(envelope{Errors: []gqlErr{{Message: "entity not found"}}})
	}))
	t.Cleanup(srv.Close)

	g := newTestGatherers(t, dir, srv.URL)
	strategy := NewImplementationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:    "req-im-6",
		TaskType:     TaskTypeImplementation,
		SpecEntityID: "semspec.spec.missing",
		GraphReady:   true,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	// Graph error on required spec → Error field set, no panic.
	if result.Error == "" {
		t.Error("expected result.Error to be set when spec entity fetch fails")
	}
}

func TestImplementationStrategy_BuildRelatedPatternsFromGraph(t *testing.T) {
	dir := t.TempDir()

	funcIDsResp := map[string]any{
		"entitiesByPredicate": []any{"code.function.authLogin"},
	}
	entityResp := map[string]any{
		"entity": map[string]any{
			"id":      "code.function.authLogin",
			"triples": []any{},
		},
	}
	emptyResp := map[string]any{"entitiesByPredicate": []any{}}

	// findRelatedPatterns queries ALL entity types before returning, THEN
	// the strategy loop calls HydrateEntity for each match. So the order is:
	//   req 1: entitiesByPredicate("code.function") → [authLogin]
	//   req 2: GetEntity("authLogin") [internal in QueryEntitiesByPredicate]
	//   req 3: entitiesByPredicate("code.type") → empty
	//   req 4: HydrateEntity("authLogin") [called by strategy loop]
	srv := newGraphQLServer(t, []map[string]any{
		funcIDsResp, // req 1: entitiesByPredicate("code.function")
		entityResp,  // req 2: GetEntity(authLogin) inside QueryEntitiesByPredicate
		emptyResp,   // req 3: entitiesByPredicate("code.type")
		entityResp,  // req 4: HydrateEntity("authLogin") called by strategy
	})

	g := newTestGatherers(t, dir, srv.URL)
	strategy := NewImplementationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-im-7",
		TaskType:   TaskTypeImplementation,
		Topic:      "auth",
		GraphReady: true,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	found := false
	for k := range result.Documents {
		if strings.Contains(k, "authLogin") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected authLogin pattern entity in Documents")
	}
}

func TestImplementationStrategy_BuildBudgetEnforced(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "big.go", strings.Repeat("// comment\n", 500))

	g := newTestGatherers(t, dir, "")
	strategy := NewImplementationStrategy(g, nil)
	budget := NewBudgetAllocation(100) // tiny

	req := &ContextBuildRequest{
		RequestID:  "req-im-8",
		TaskType:   TaskTypeImplementation,
		Files:      []string{"big.go"},
		GraphReady: false,
	}

	_, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if budget.Allocated > budget.Total {
		t.Errorf("budget over-allocated: %d > %d", budget.Allocated, budget.Total)
	}
}

// ---------------------------------------------------------------------------
// ReviewStrategy.Build() tests
// ---------------------------------------------------------------------------

func TestReviewStrategy_BuildFindsTestFiles(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "auth/handler.go", "package auth\n\nfunc Handle() {}")
	writeFile(t, dir, "auth/handler_test.go", "package auth\n\nfunc TestHandle(t *testing.T) {}")

	g := newTestGatherers(t, dir, "")
	strategy := NewReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-rv-1",
		TaskType:   TaskTypeReview,
		Files:      []string{"auth/handler.go"},
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if _, ok := result.Documents["auth/handler_test.go"]; !ok {
		t.Error("expected auth/handler_test.go in Documents")
	}
}

func TestReviewStrategy_BuildInfersDomains(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	g := newTestGatherers(t, dir, "")
	strategy := NewReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-rv-2",
		TaskType:   TaskTypeReview,
		Files:      []string{"auth/session.go", "api/handler.go"},
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	domainSet := make(map[string]bool)
	for _, d := range result.Domains {
		domainSet[d] = true
	}
	if !domainSet["auth"] {
		t.Errorf("expected 'auth' domain inferred from auth/session.go; got %v", result.Domains)
	}
	if !domainSet["api"] {
		t.Errorf("expected 'api' domain inferred from api/handler.go; got %v", result.Domains)
	}
}

func TestReviewStrategy_BuildIncludesConventionFiles(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "CONVENTIONS.md", "# Conventions\n\nUse gofmt and golint.")

	g := newTestGatherers(t, dir, "")
	strategy := NewReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-rv-3",
		TaskType:   TaskTypeReview,
		Files:      []string{"main.go"},
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if _, ok := result.Documents["CONVENTIONS.md"]; !ok {
		t.Error("expected CONVENTIONS.md in Documents")
	}
}

func TestReviewStrategy_BuildNoFilesNoDiff(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	g := newTestGatherers(t, dir, "")
	strategy := NewReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-rv-4",
		TaskType:   TaskTypeReview,
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if result.Diffs != "" {
		t.Error("expected empty Diffs when no files and no GitRef")
	}
}

func TestReviewStrategy_BuildWithGitDiff(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, dir, "service.go", "package main\n\nfunc Foo() {}")
	runGit(t, dir, "add", "service.go")
	runGit(t, dir, "commit", "-m", "initial commit")

	// Modify file to create a working-tree diff.
	writeFile(t, dir, "service.go", "package main\n\nfunc Foo() { /* modified */ }")

	g := newTestGatherers(t, dir, "")
	strategy := NewReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-rv-5",
		TaskType:   TaskTypeReview,
		Files:      []string{"service.go"},
		GitRef:     "HEAD",
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if budget.Allocated > budget.Total {
		t.Errorf("budget over-allocated: %d > %d", budget.Allocated, budget.Total)
	}
	// Diffs presence depends on git state; we only assert no panic and budget respected.
	_ = result.Diffs
}

func TestReviewStrategy_BuildTinyBudgetTruncatesDiff(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	bigContent := strings.Repeat("// original line\n", 300)
	writeFile(t, dir, "big.go", bigContent)
	runGit(t, dir, "add", "big.go")
	runGit(t, dir, "commit", "-m", "add big file")

	writeFile(t, dir, "big.go", strings.Repeat("// modified line\n", 300))

	g := newTestGatherers(t, dir, "")
	strategy := NewReviewStrategy(g, nil)
	budget := NewBudgetAllocation(50) // tiny — diff must be truncated

	req := &ContextBuildRequest{
		RequestID:  "req-rv-6",
		TaskType:   TaskTypeReview,
		Files:      []string{"big.go"},
		GitRef:     "HEAD",
		GraphReady: false,
	}

	_, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if budget.Allocated > budget.Total {
		t.Errorf("budget over-allocated: %d > %d", budget.Allocated, budget.Total)
	}
}

// ---------------------------------------------------------------------------
// ExplorationStrategy.Build() tests
// ---------------------------------------------------------------------------

func TestExplorationStrategy_BuildIncludesRelatedDocs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Project\n\nAuthentication via JWT.")

	g := newTestGatherers(t, dir, "")
	strategy := NewExplorationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-ex-1",
		TaskType:   TaskTypeExploration,
		Topic:      "authentication",
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if _, ok := result.Documents["README.md"]; !ok {
		t.Error("expected README.md in Documents")
	}
}

func TestExplorationStrategy_BuildNoTopicNoGraph(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewExplorationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-ex-2",
		TaskType:   TaskTypeExploration,
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestExplorationStrategy_BuildWithRequestedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.go", "package config\n\nvar DB = \"postgres\"")

	g := newTestGatherers(t, dir, "")
	strategy := NewExplorationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-ex-3",
		TaskType:   TaskTypeExploration,
		Files:      []string{"config.go"},
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if _, ok := result.Documents["config.go"]; !ok {
		t.Error("expected config.go in Documents")
	}
}

func TestExplorationStrategy_BuildWithGraphEntities(t *testing.T) {
	dir := t.TempDir()

	empty := map[string]any{"entitiesByPredicate": []any{}}
	authFuncResp := map[string]any{
		"entitiesByPredicate": []any{"code.function.authVerify"},
	}
	entityHydrated := map[string]any{
		"entity": map[string]any{
			"id":      "code.function.authVerify",
			"triples": []any{},
		},
	}

	// Request order for ExplorationStrategy.Build("auth"):
	//   reqs 1-4:  GetCodebaseSummary (4 empty entitiesByPredicate)
	//   req  5:    entitiesByPredicate("code.function") → [authVerify]
	//   req  6:    GetEntity("authVerify") [internal in QueryEntitiesByPredicate]
	//   reqs 7-10: entitiesByPredicate for code.type, code.interface, code.package, semspec.plan
	//   req  11:   HydrateEntity("authVerify") [strategy loop, AFTER all findMatchingEntities returns]
	srv := newGraphQLServer(t, []map[string]any{
		empty, empty, empty, empty, // reqs 1-4: GetCodebaseSummary (all empty)
		authFuncResp,               // req 5: entitiesByPredicate("code.function")
		entityHydrated,             // req 6: GetEntity(authVerify) internal
		empty, empty, empty, empty, // reqs 7-10: code.type, code.interface, code.package, semspec.plan
		entityHydrated, // req 11: HydrateEntity by strategy
	})

	g := newTestGatherers(t, dir, srv.URL)
	strategy := NewExplorationStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-ex-4",
		TaskType:   TaskTypeExploration,
		Topic:      "auth",
		GraphReady: true,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	found := false
	for k := range result.Documents {
		if strings.Contains(k, "authVerify") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected authVerify entity from graph in Documents")
	}
}

func TestExplorationStrategy_BuildBudgetEnforced(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", strings.Repeat("line content\n", 400))

	g := newTestGatherers(t, dir, "")
	strategy := NewExplorationStrategy(g, nil)
	budget := NewBudgetAllocation(50)

	req := &ContextBuildRequest{
		RequestID:  "req-ex-5",
		TaskType:   TaskTypeExploration,
		GraphReady: false,
	}

	_, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if budget.Allocated > budget.Total {
		t.Errorf("budget over-allocated: %d > %d", budget.Allocated, budget.Total)
	}
}

// ---------------------------------------------------------------------------
// QuestionStrategy.Build() tests
// ---------------------------------------------------------------------------

func TestQuestionStrategy_BuildNoTopicGeneratesBlockingQuestion(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewQuestionStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-qs-1",
		TaskType:   TaskTypeQuestion,
		WorkflowID: "wf-abc",
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	found := false
	for _, q := range result.Questions {
		if q.Topic == "requirements.clarification" && q.Urgency == UrgencyBlocking {
			found = true
		}
	}
	if !found {
		t.Errorf("expected blocking requirements.clarification question; got: %+v", result.Questions)
	}
	if !result.InsufficientContext {
		t.Error("InsufficientContext should be true when topic is missing")
	}
}

func TestQuestionStrategy_BuildTopicWithMatchingDoc(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Project\n\nAuthentication is handled via JWT tokens.")

	g := newTestGatherers(t, dir, "")
	strategy := NewQuestionStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-qs-2",
		TaskType:   TaskTypeQuestion,
		Topic:      "authentication JWT",
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if _, ok := result.Documents["README.md"]; !ok {
		t.Error("expected README.md to be included as it matches the topic")
	}
	// A blocking question should not be present when docs match.
	for _, q := range result.Questions {
		if q.Urgency == UrgencyBlocking {
			t.Errorf("unexpected blocking question when docs are available: %+v", q)
		}
	}
}

func TestQuestionStrategy_BuildTopicNoKnowledge(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	strategy := NewQuestionStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-qs-3",
		TaskType:   TaskTypeQuestion,
		Topic:      "quantum-teleportation",
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if !result.InsufficientContext {
		t.Error("InsufficientContext should be true when nothing matches")
	}
	found := false
	for _, q := range result.Questions {
		if strings.HasPrefix(q.Topic, "knowledge.") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected knowledge.* question; got: %+v", result.Questions)
	}
}

func TestQuestionStrategy_BuildWithRequestedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "notes.md", "# Notes\n\nDatabase schema design.")

	g := newTestGatherers(t, dir, "")
	strategy := NewQuestionStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-qs-4",
		TaskType:   TaskTypeQuestion,
		Topic:      "database schema",
		Files:      []string{"notes.md"},
		GraphReady: false,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if _, ok := result.Documents["notes.md"]; !ok {
		t.Error("expected notes.md in Documents for explicitly requested file")
	}
}

func TestQuestionStrategy_BuildWithGraphEntities(t *testing.T) {
	dir := t.TempDir()

	authEntityID := "code.function.authVerify"
	entitiesResp := map[string]any{
		"entitiesByPredicate": []any{authEntityID},
	}
	entityHydrated := map[string]any{
		"entity": map[string]any{
			"id":      authEntityID,
			"triples": []any{},
		},
	}
	empty := map[string]any{"entitiesByPredicate": []any{}}

	// Request sequence for QuestionStrategy.Build("auth verify") with GraphReady=true:
	//
	// addMatchingEntities → findMatchingEntities → queries ALL prefixes first, THEN returns:
	//   req 1: entitiesByPredicate("code.function") → [authVerify]
	//   req 2: GetEntity("authVerify") [internal in QueryEntitiesByPredicate]
	//   reqs 3-7: entitiesByPredicate for code.type, code.interface, code.package,
	//             semspec.plan, semspec.spec (all empty)
	// Back in strategy loop:
	//   req 8: HydrateEntity("authVerify")
	//
	// addSourceDocuments → findSourceDocuments:
	//   req 9: entitiesByPredicate("source.doc") → empty
	//
	// GetCodebaseSummary (4 queries):
	//   reqs 10-13: entitiesByPredicate for Functions/Types/Interfaces/Packages
	srv := newGraphQLServer(t, []map[string]any{
		entitiesResp,        // req 1: entitiesByPredicate("code.function")
		entityHydrated,      // req 2: GetEntity(authVerify) internal
		empty, empty, empty, // reqs 3-5: code.type, code.interface, code.package
		empty, empty, // reqs 6-7: semspec.plan, semspec.spec
		entityHydrated,             // req 8: HydrateEntity by strategy
		empty,                      // req 9: source.doc
		empty, empty, empty, empty, // reqs 10-13: GetCodebaseSummary
	})

	g := newTestGatherers(t, dir, srv.URL)
	strategy := NewQuestionStrategy(g, nil)
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-qs-5",
		TaskType:   TaskTypeQuestion,
		Topic:      "auth verify",
		GraphReady: true,
	}

	result, err := strategy.Build(context.Background(), req, budget)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	found := false
	for k := range result.Documents {
		if strings.Contains(k, "authVerify") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected authVerify entity in Documents from graph")
	}
}

// ---------------------------------------------------------------------------
// StrategyFactory tests
// ---------------------------------------------------------------------------

func TestStrategyFactory_CreatesAllStrategies(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	factory := NewStrategyFactory(g, nil)

	taskTypes := []TaskType{
		TaskTypeReview,
		TaskTypeImplementation,
		TaskTypeExploration,
		TaskTypePlanReview,
		TaskTypePlanning,
		TaskTypeQuestion,
	}

	for _, tt := range taskTypes {
		t.Run(string(tt), func(t *testing.T) {
			s := factory.Create(tt)
			if s == nil {
				t.Errorf("Create(%q) returned nil", tt)
			}
		})
	}
}

func TestStrategyFactory_UnknownTypeDefaultsToExploration(t *testing.T) {
	dir := t.TempDir()
	g := newTestGatherers(t, dir, "")
	factory := NewStrategyFactory(g, nil)

	s := factory.Create("nonsense-type")
	if s == nil {
		t.Fatal("Create with unknown type should not return nil")
	}
	// We cannot type-assert the interface to *ExplorationStrategy from outside
	// the package but we can verify it builds without error.
	budget := NewBudgetAllocation(1000)
	req := &ContextBuildRequest{
		RequestID:  "req-factory-1",
		TaskType:   TaskTypeExploration,
		GraphReady: false,
	}
	_, err := s.Build(context.Background(), req, budget)
	if err != nil {
		t.Errorf("default strategy Build() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// formatPlanContent helper tests
// ---------------------------------------------------------------------------

func TestFormatPlanContentWithSlug(t *testing.T) {
	out := formatPlanContent("my-slug", `{"key":"value"}`)
	if !strings.Contains(out, "my-slug") {
		t.Error("expected slug in output")
	}
	if !strings.Contains(out, "```json") {
		t.Error("expected JSON code block fence")
	}
	if !strings.Contains(out, `{"key":"value"}`) {
		t.Error("expected plan content in output")
	}
}

func TestFormatPlanContentNoSlug(t *testing.T) {
	out := formatPlanContent("", `{"key":"value"}`)
	if strings.Contains(out, "**Slug:**") {
		t.Error("should not include Slug line when slug is empty")
	}
}

// ---------------------------------------------------------------------------
// Cross-cutting: context cancellation
// ---------------------------------------------------------------------------

func TestStrategiesRespectContextCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main")

	g := newTestGatherers(t, dir, "")
	budget := NewBudgetAllocation(50000)

	req := &ContextBuildRequest{
		RequestID:  "req-ctx-1",
		TaskType:   TaskTypePlanning,
		Topic:      "test",
		GraphReady: false,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Strategies should not panic on cancelled context; they may return partial results.
	strategy := NewPlanningStrategy(g, nil)
	result, err := strategy.Build(ctx, req, budget)
	// Either a result or a context error is acceptable — no panic, no data race.
	if err != nil && result != nil {
		t.Logf("Build with cancelled context: err=%v, result non-nil (acceptable)", err)
	}
}
