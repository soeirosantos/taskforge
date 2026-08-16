package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"taskforge/internal/jobs"
)

// jobDTO decodes the SPEC 11 job representation as a client sees it. It is
// deliberately a separate type from jobs.Job rather than a reuse of it: these
// tests assert the wire shape (field names, and JSON null for absent values),
// which reusing the domain type's own UnmarshalJSON would hide.
type jobDTO struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Status       string          `json:"status"`
	Payload      json.RawMessage `json:"payload"`
	Result       json.RawMessage `json:"result"`
	Err          json.RawMessage `json:"error"`
	AttemptCount int             `json:"attempt_count"`
	CreatedAt    string          `json:"created_at"`
	QueuedAt     string          `json:"queued_at"`
	StartedAt    *string         `json:"started_at"`
	FinishedAt   *string         `json:"finished_at"`
	UpdatedAt    string          `json:"updated_at"`
}

// isJSONNull reports whether a decoded raw field was absent or JSON null.
func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

// decodeJob parses a single SPEC 11 job object from a response body.
func decodeJob(t *testing.T, body []byte) jobDTO {
	t.Helper()
	var j jobDTO
	if err := json.Unmarshal(body, &j); err != nil {
		t.Fatalf("decode job: %v; body=%s", err, body)
	}
	return j
}

// getJobDTO fetches a job through GET /jobs/{id}, so assertions about a job's
// final persisted state go through the API rather than peeking at the store.
func getJobDTO(t *testing.T, h http.Handler, id string) jobDTO {
	t.Helper()
	rec := doRequest(h, http.MethodGet, "/jobs/"+id, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get job %s: status = %d, body=%s", id, rec.Code, rec.Body.String())
	}
	return decodeJob(t, rec.Body.Bytes())
}

type listJobsDTO struct {
	Jobs []jobDTO `json:"jobs"`
}

func decodeListJobs(t *testing.T, body []byte) listJobsDTO {
	t.Helper()
	var got listJobsDTO
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode list response: %v; body=%s", err, body)
	}
	return got
}

func createJobDTO(t *testing.T, h http.Handler, jobType, payload string) jobDTO {
	t.Helper()
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"`+jobType+`","payload":`+payload+`}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s job: status = %d, body=%s", jobType, rec.Code, rec.Body.String())
	}
	var j jobDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
		t.Fatalf("decode created job: %v", err)
	}
	return j
}

// --- List -------------------------------------------------------------

func TestListJobs_EmptyIsEmptyArrayNotNull(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodGet, "/jobs", "", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if rec.Body.String() != `{"jobs":[]}` {
		t.Errorf("body = %s, want {\"jobs\":[]}", rec.Body.String())
	}
}

func TestListJobs_NoFilter(t *testing.T) {
	h, _ := newTestHandler(t)
	a := createJobDTO(t, h, "hash", `{"text":"a"}`)
	b := createJobDTO(t, h, "delay", `{"milliseconds":100}`)
	c := createJobDTO(t, h, "fail", `{}`)

	rec := doRequest(h, http.MethodGet, "/jobs", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeListJobs(t, rec.Body.Bytes())
	if len(got.Jobs) != 3 {
		t.Fatalf("len(jobs) = %d, want 3; body=%s", len(got.Jobs), rec.Body.String())
	}

	seen := map[string]bool{}
	for _, j := range got.Jobs {
		seen[j.ID] = true
	}
	for _, id := range []string{a.ID, b.ID, c.ID} {
		if !seen[id] {
			t.Errorf("job %s missing from unfiltered list", id)
		}
	}
}

func TestListJobs_StatusFilter(t *testing.T) {
	h, s := newTestHandler(t)
	ctx := context.Background()

	// toFail is created first on purpose: SPEC 20 makes Claim take the
	// oldest queued job (queued_at ASC), so creating it first is what makes
	// the claim below deterministic.
	toFail := createJobDTO(t, h, "hash", `{"text":"b"}`)
	queued := createJobDTO(t, h, "hash", `{"text":"a"}`)

	// Drive toFail to FAILED directly through the store, exactly as SPEC 22/23
	// would via a worker: Claim (QUEUED -> RUNNING) then Fail (RUNNING ->
	// FAILED).
	claimed, err := s.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.ID != toFail.ID {
		t.Fatalf("claimed %s, want %s (SPEC 20 ordering assumption broken)", claimed.ID, toFail.ID)
	}
	if _, _, err := s.Fail(ctx, toFail.ID, jobs.IntentionalFailure()); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	rec := doRequest(h, http.MethodGet, "/jobs?status=FAILED", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeListJobs(t, rec.Body.Bytes())
	if len(got.Jobs) != 1 || got.Jobs[0].ID != toFail.ID {
		t.Fatalf("status=FAILED jobs = %+v, want only %s", got.Jobs, toFail.ID)
	}

	rec = doRequest(h, http.MethodGet, "/jobs?status=QUEUED", "", nil)
	got = decodeListJobs(t, rec.Body.Bytes())
	if len(got.Jobs) != 1 || got.Jobs[0].ID != queued.ID {
		t.Fatalf("status=QUEUED jobs = %+v, want only %s", got.Jobs, queued.ID)
	}
}

func TestListJobs_TypeFilter(t *testing.T) {
	h, _ := newTestHandler(t)
	hashJob := createJobDTO(t, h, "hash", `{"text":"a"}`)
	createJobDTO(t, h, "delay", `{"milliseconds":100}`)

	rec := doRequest(h, http.MethodGet, "/jobs?type=hash", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeListJobs(t, rec.Body.Bytes())
	if len(got.Jobs) != 1 || got.Jobs[0].ID != hashJob.ID {
		t.Fatalf("type=hash jobs = %+v, want only %s", got.Jobs, hashJob.ID)
	}
}

func TestListJobs_CombinedFilters(t *testing.T) {
	h, s := newTestHandler(t)
	ctx := context.Background()

	hashToFail := createJobDTO(t, h, "hash", `{"text":"a"}`)
	createJobDTO(t, h, "delay", `{"milliseconds":100}`) // stays QUEUED, wrong type
	createJobDTO(t, h, "hash", `{"text":"b"}`)          // stays QUEUED, wrong status

	claimed, err := s.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.ID != hashToFail.ID {
		t.Fatalf("claimed %s, want %s (SPEC 20 ordering assumption broken)", claimed.ID, hashToFail.ID)
	}
	if _, _, err := s.Fail(ctx, hashToFail.ID, jobs.IntentionalFailure()); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	rec := doRequest(h, http.MethodGet, "/jobs?status=FAILED&type=hash", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeListJobs(t, rec.Body.Bytes())
	if len(got.Jobs) != 1 || got.Jobs[0].ID != hashToFail.ID {
		t.Fatalf("status=FAILED&type=hash jobs = %+v, want only %s", got.Jobs, hashToFail.ID)
	}
}

func TestListJobs_InvalidStatus(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodGet, "/jobs?status=BOGUS", "", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_FILTER" {
		t.Errorf("code = %q, want INVALID_FILTER", e.Error.Code)
	}
}

func TestListJobs_InvalidType(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodGet, "/jobs?type=bogus", "", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_FILTER" {
		t.Errorf("code = %q, want INVALID_FILTER", e.Error.Code)
	}
}

func TestListJobs_UnknownQueryParamNameIsIgnored(t *testing.T) {
	h, _ := newTestHandler(t)
	created := createJobDTO(t, h, "hash", `{"text":"a"}`)

	rec := doRequest(h, http.MethodGet, "/jobs?foo=bar", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeListJobs(t, rec.Body.Bytes())
	if len(got.Jobs) != 1 || got.Jobs[0].ID != created.ID {
		t.Fatalf("jobs = %+v, want only %s", got.Jobs, created.ID)
	}
}

func TestListJobs_DeterministicOrdering(t *testing.T) {
	h, _ := newTestHandler(t)
	first := createJobDTO(t, h, "hash", `{"text":"a"}`)
	time.Sleep(2 * time.Millisecond)
	second := createJobDTO(t, h, "hash", `{"text":"b"}`)
	time.Sleep(2 * time.Millisecond)
	third := createJobDTO(t, h, "hash", `{"text":"c"}`)

	rec := doRequest(h, http.MethodGet, "/jobs", "", nil)
	got := decodeListJobs(t, rec.Body.Bytes())
	if len(got.Jobs) != 3 {
		t.Fatalf("len(jobs) = %d, want 3", len(got.Jobs))
	}

	// created_at DESC: newest (third) first, oldest (first) last.
	wantOrder := []string{third.ID, second.ID, first.ID}
	for i, id := range wantOrder {
		if got.Jobs[i].ID != id {
			t.Errorf("position %d: id = %q, want %q (full order: %v)", i, got.Jobs[i].ID, id, got.Jobs)
		}
	}
}

// TestListJobs_OrderingTiebreakByID constructs two jobs with an identical
// created_at (bypassing the API, which cannot force a tie) and verifies the
// id-ascending tiebreak SPEC 19 requires is genuinely exercised, not just
// coincidentally satisfied by monotonically increasing timestamps.
func TestListJobs_OrderingTiebreakByID(t *testing.T) {
	h, s := newTestHandler(t)
	ctx := context.Background()

	tie := time.Now().UTC()
	j1, err := jobs.NewJob(jobs.TypeHash, json.RawMessage(`{"text":"a"}`), tie)
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	j2, err := jobs.NewJob(jobs.TypeHash, json.RawMessage(`{"text":"b"}`), tie)
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if !j1.CreatedAt.Equal(j2.CreatedAt) {
		t.Fatalf("test setup: CreatedAt not equal (%v vs %v)", j1.CreatedAt, j2.CreatedAt)
	}

	// Ids are random, so which of the two sorts first is decided here, and
	// the pair is then inserted in the *opposite* order. That is what makes
	// the assertion discriminating: without the id tiebreak the two rows are
	// equal under "created_at DESC" alone and come back in insertion order,
	// which is deliberately the wrong answer.
	first, second := j1, j2
	if second.ID < first.ID {
		first, second = second, first
	}
	if err := s.Create(ctx, second); err != nil {
		t.Fatalf("Create %s: %v", second.ID, err)
	}
	if err := s.Create(ctx, first); err != nil {
		t.Fatalf("Create %s: %v", first.ID, err)
	}

	wantFirst, wantSecond := first.ID, second.ID

	rec := doRequest(h, http.MethodGet, "/jobs", "", nil)
	got := decodeListJobs(t, rec.Body.Bytes())
	if len(got.Jobs) != 2 {
		t.Fatalf("len(jobs) = %d, want 2", len(got.Jobs))
	}
	if got.Jobs[0].ID != wantFirst || got.Jobs[1].ID != wantSecond {
		t.Errorf("order = [%s, %s], want [%s, %s] (id ascending within the created_at tie)",
			got.Jobs[0].ID, got.Jobs[1].ID, wantFirst, wantSecond)
	}
}
