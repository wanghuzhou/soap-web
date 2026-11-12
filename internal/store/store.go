// Package store persists projects to a local JSON file and provides CRUD over
// the project -> service -> operation -> call hierarchy.
//
// It is safe for concurrent use: callers receive deep copies, and every
// mutation is applied to a copy, persisted, and only then swapped into memory.
// Writes are atomic (temp file + rename), so a crash cannot truncate the file.
package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"soapweb/internal/fileutil"
	"soapweb/internal/model"
)

// ErrNotFound is returned when a referenced node does not exist.
var ErrNotFound = errors.New("未找到指定对象")

// Store is the persistent project repository.
type Store struct {
	// mu guards projects for readers and the final in-memory swap.
	mu sync.RWMutex
	// writeMu serializes the clone -> write -> swap cycle, so JSON encoding and
	// disk I/O never block readers.
	writeMu  sync.Mutex
	path     string
	projects []model.Project
}

// New loads (or initialises) the store at the given file path.
func New(path string) (*Store, error) {
	s := &Store{path: path}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil // start empty
		}
		return nil, err
	}
	// Ignore a UTF-8 BOM: Windows editors add one by default and
	// encoding/json rejects it.
	data = fileutil.StripBOM(data)
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &s.projects); err != nil {
			return nil, err
		}
	}
	// Older WSDL imports stored operations without any request sample. Opening
	// such a project showed an empty editor, which looks like a failed import.
	// Backfill one sample from the generated template and persist the fix.
	if backfillWSDLCalls(s.projects) {
		if err := s.saveProjects(s.projects); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// backfillWSDLCalls gives legacy WSDL-imported operations a default request
// sample when the operation carries a generated template but no call. It is
// idempotent: projects that already have calls are left untouched.
func backfillWSDLCalls(projects []model.Project) bool {
	changed := false
	for pi := range projects {
		project := &projects[pi]
		if project.Source != "wsdl" && project.Source != "wsdl-url" {
			continue
		}
		for si := range project.Services {
			for oi := range project.Services[si].Operations {
				op := &project.Services[si].Operations[oi]
				if len(op.Calls) > 0 {
					continue
				}
				call, ok := model.SeedCall(op)
				if !ok {
					continue
				}
				call.ID = NewID()
				op.Calls = []model.Call{call}
				changed = true
			}
		}
	}
	return changed
}

// NewID returns a short random hex identifier.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// saveProjects persists the project list atomically. Callers either run before
// the store is shared (New) or hold writeMu (commit).
func (s *Store) saveProjects(projects []model.Project) error {
	return fileutil.WriteJSONAtomic(s.path, projects, 0o644)
}

// commit applies mutate to a deep copy, persists it, and only then swaps it
// into memory, so a failed write leaves memory unchanged. Writers are
// serialized by writeMu; readers keep serving the previous snapshot while the
// file is being encoded and written.
func (s *Store) commit(mutate func([]model.Project) ([]model.Project, error)) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.RLock()
	next := cloneProjects(s.projects)
	s.mu.RUnlock()

	next, err := mutate(next)
	if err != nil {
		return err
	}
	if err := s.saveProjects(next); err != nil {
		return err
	}

	s.mu.Lock()
	s.projects = next
	s.mu.Unlock()
	return nil
}

// ---- deep copy helpers ----

// cloneProject returns a project that shares no mutable state with p.
func cloneProject(p model.Project) model.Project {
	out := p
	if p.Proxy != nil {
		proxy := *p.Proxy
		out.Proxy = &proxy
	}
	out.Headers = cloneHeaders(p.Headers)
	if p.Services != nil {
		out.Services = make([]model.Service, len(p.Services))
		for i, svc := range p.Services {
			out.Services[i] = cloneService(svc)
		}
	}
	if p.TestSuites != nil {
		out.TestSuites = make([]model.TestSuite, len(p.TestSuites))
		for i, ts := range p.TestSuites {
			out.TestSuites[i] = cloneTestSuite(ts)
		}
	}
	return out
}

// cloneHeaders copies a header slice, preserving nil so an untouched project
// keeps its JSON shape (omitempty).
func cloneHeaders(in []model.Header) []model.Header {
	if in == nil {
		return nil
	}
	return append([]model.Header(nil), in...)
}

func cloneService(svc model.Service) model.Service {
	out := svc
	if svc.Operations != nil {
		out.Operations = make([]model.Operation, len(svc.Operations))
		for i, op := range svc.Operations {
			out.Operations[i] = cloneOperation(op)
		}
	}
	return out
}

func cloneOperation(op model.Operation) model.Operation {
	out := op
	if op.Calls != nil {
		out.Calls = make([]model.Call, len(op.Calls))
		for i, c := range op.Calls {
			out.Calls[i] = cloneCall(c)
		}
	}
	return out
}

func cloneCall(c model.Call) model.Call {
	out := c
	out.Headers = cloneHeaders(c.Headers)
	out.Response = cloneSavedResponse(c.Response)
	return out
}

// cloneSavedResponse deep-copies the response and its header map, preserving nil
// so an untouched sample keeps its JSON shape.
func cloneSavedResponse(r *model.SavedResponse) *model.SavedResponse {
	if r == nil {
		return nil
	}
	out := *r
	if r.Headers != nil {
		out.Headers = make(map[string]string, len(r.Headers))
		for k, v := range r.Headers {
			out.Headers[k] = v
		}
	}
	return &out
}

func cloneTestSuite(ts model.TestSuite) model.TestSuite {
	out := ts
	if ts.TestCases != nil {
		out.TestCases = make([]model.TestCase, len(ts.TestCases))
		for i, tc := range ts.TestCases {
			c := tc
			if tc.Steps != nil {
				c.Steps = make([]model.TestStep, len(tc.Steps))
				copy(c.Steps, tc.Steps)
			}
			out.TestCases[i] = c
		}
	}
	return out
}

func cloneProjects(in []model.Project) []model.Project {
	if in == nil {
		return nil
	}
	out := make([]model.Project, len(in))
	for i, p := range in {
		out[i] = cloneProject(p)
	}
	return out
}

// indexOfProject returns the index of a project by id, or -1.
func indexOfProject(list []model.Project, id string) int {
	for i := range list {
		if list[i].ID == id {
			return i
		}
	}
	return -1
}

// locateService returns the indexes of a service within a project. Callers hold
// the store lock; ok=false means the project or service does not exist.
func locateService(list []model.Project, projectID, serviceID string) (pi, si int, ok bool) {
	pi = indexOfProject(list, projectID)
	if pi < 0 {
		return 0, 0, false
	}
	for si = range list[pi].Services {
		if list[pi].Services[si].ID == serviceID {
			return pi, si, true
		}
	}
	return 0, 0, false
}

// locateOperation returns the indexes of an operation within a service.
func locateOperation(list []model.Project, projectID, serviceID, opID string) (pi, si, oi int, ok bool) {
	pi, si, ok = locateService(list, projectID, serviceID)
	if !ok {
		return 0, 0, 0, false
	}
	for oi = range list[pi].Services[si].Operations {
		if list[pi].Services[si].Operations[oi].ID == opID {
			return pi, si, oi, true
		}
	}
	return 0, 0, 0, false
}

// List returns a deep copy of all projects. An empty store returns [] rather
// than null, since "no projects yet" is a normal state.
func (s *Store) List() []model.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.projects == nil {
		return []model.Project{}
	}
	return cloneProjects(s.projects)
}

// Get returns a deep copy of one project by id.
func (s *Store) Get(id string) (model.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if i := indexOfProject(s.projects, id); i >= 0 {
		return cloneProject(s.projects[i]), nil
	}
	return model.Project{}, ErrNotFound
}

// ---- project-level CRUD ----

// AddProject appends a project, assigning an id if missing, and saves.
func (s *Store) AddProject(p model.Project) (model.Project, error) {
	if err := p.Proxy.Validate(); err != nil {
		return p, err
	}
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.Name == "" {
		p.Name = "未命名工程"
	}
	EnsureIDs(&p)
	err := s.commit(func(list []model.Project) ([]model.Project, error) {
		return append(list, cloneProject(p)), nil
	})
	return p, err
}

// UpdateProject replaces a whole project (matched by id) and saves.
func (s *Store) UpdateProject(p model.Project) error {
	if err := p.Proxy.Validate(); err != nil {
		return err
	}
	// A full replace must leave the same invariant as AddProject: every node
	// reachable by id. Without this a client that omits an id silently persists
	// a node that no CRUD route can address afterwards.
	EnsureIDs(&p)
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		i := indexOfProject(list, p.ID)
		if i < 0 {
			return nil, ErrNotFound
		}
		list[i] = cloneProject(p)
		return list, nil
	})
}

// UpdateProjectProxy updates only the proxy settings. A narrow write avoids
// overwriting concurrent edits from another tab with a stale snapshot.
func (s *Store) UpdateProjectProxy(id string, proxy *model.ProxySettings) error {
	if err := proxy.Validate(); err != nil {
		return err
	}
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		i := indexOfProject(list, id)
		if i < 0 {
			return nil, ErrNotFound
		}
		if proxy == nil {
			list[i].Proxy = nil
		} else {
			p := *proxy
			list[i].Proxy = &p
		}
		return list, nil
	})
}

// RenameProject updates only a project's name.
func (s *Store) RenameProject(id, name string) error {
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		i := indexOfProject(list, id)
		if i < 0 {
			return nil, ErrNotFound
		}
		list[i].Name = name
		return list, nil
	})
}

// DeleteProject removes a project by id.
func (s *Store) DeleteProject(id string) error {
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		i := indexOfProject(list, id)
		if i < 0 {
			return nil, ErrNotFound
		}
		return append(list[:i], list[i+1:]...), nil
	})
}

// ---- nested CRUD helpers ----

// AddService appends a service to a project.
func (s *Store) AddService(projectID string, svc model.Service) (model.Service, error) {
	if svc.ID == "" {
		svc.ID = NewID()
	}
	for i := range svc.Operations {
		assignOperationIDs(&svc.Operations[i])
	}
	err := s.commit(func(list []model.Project) ([]model.Project, error) {
		pi := indexOfProject(list, projectID)
		if pi < 0 {
			return nil, ErrNotFound
		}
		list[pi].Services = append(list[pi].Services, cloneService(svc))
		return list, nil
	})
	return svc, err
}

// DeleteService removes a service from a project.
func (s *Store) DeleteService(projectID, serviceID string) error {
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		pi, si, ok := locateService(list, projectID, serviceID)
		if !ok {
			return nil, ErrNotFound
		}
		list[pi].Services = append(list[pi].Services[:si], list[pi].Services[si+1:]...)
		return list, nil
	})
}

// AddOperation appends an operation to a service.
func (s *Store) AddOperation(projectID, serviceID string, op model.Operation) (model.Operation, error) {
	assignOperationIDs(&op)
	err := s.commit(func(list []model.Project) ([]model.Project, error) {
		pi, si, ok := locateService(list, projectID, serviceID)
		if !ok {
			return nil, ErrNotFound
		}
		list[pi].Services[si].Operations = append(list[pi].Services[si].Operations, cloneOperation(op))
		return list, nil
	})
	return op, err
}

// DeleteOperation removes an operation from a service.
func (s *Store) DeleteOperation(projectID, serviceID, opID string) error {
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		pi, si, oi, ok := locateOperation(list, projectID, serviceID, opID)
		if !ok {
			return nil, ErrNotFound
		}
		svc := &list[pi].Services[si]
		svc.Operations = append(svc.Operations[:oi], svc.Operations[oi+1:]...)
		return list, nil
	})
}

// AddCall appends a request sample (call) to an operation.
func (s *Store) AddCall(projectID, serviceID, opID string, call model.Call) (model.Call, error) {
	if call.ID == "" {
		call.ID = NewID()
	}
	if call.Title == "" {
		call.Title = "新请求 " + time.Now().Format("15:04:05")
	}
	err := s.commit(func(list []model.Project) ([]model.Project, error) {
		pi, si, oi, ok := locateOperation(list, projectID, serviceID, opID)
		if !ok {
			return nil, ErrNotFound
		}
		op := &list[pi].Services[si].Operations[oi]
		// Seed the new request from the operation's structural template
		// (方案 A: pure skeleton from WSDL, no business data).
		if call.Request == "" {
			call.Request = op.DefaultRequestBody()
		}
		if call.Endpoint == "" {
			call.Endpoint = op.Endpoint
		}
		if call.Action == "" {
			call.Action = op.SoapAction
		}
		// cloneCall: the caller keeps ownership of the value it passed in. The
		// package promises that no mutable state is shared with callers, and
		// Headers is a slice.
		op.Calls = append(op.Calls, cloneCall(call))
		return list, nil
	})
	return call, err
}

// UpdateCall replaces a call (by id) within an operation.
func (s *Store) UpdateCall(projectID, serviceID, opID string, call model.Call) error {
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		pi, si, oi, ok := locateOperation(list, projectID, serviceID, opID)
		if !ok {
			return nil, ErrNotFound
		}
		op := &list[pi].Services[si].Operations[oi]
		for ci := range op.Calls {
			if op.Calls[ci].ID == call.ID {
				// The saved response is written only by UpdateCallResponse: a
				// request edit must not erase it by omitting the field.
				call.Response = op.Calls[ci].Response
				op.Calls[ci] = cloneCall(call)
				return list, nil
			}
		}
		return nil, ErrNotFound
	})
}

// UpdateCallResponse stores (or, with nil, clears) the saved response of one
// request sample. A narrow write keeps a response save from overwriting
// concurrent request edits made in another tab.
func (s *Store) UpdateCallResponse(projectID, serviceID, opID, callID string, resp *model.SavedResponse) error {
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		pi, si, oi, ok := locateOperation(list, projectID, serviceID, opID)
		if !ok {
			return nil, ErrNotFound
		}
		op := &list[pi].Services[si].Operations[oi]
		for ci := range op.Calls {
			if op.Calls[ci].ID == callID {
				op.Calls[ci].Response = cloneSavedResponse(resp)
				return list, nil
			}
		}
		return nil, ErrNotFound
	})
}

// DeleteCall removes a call from an operation.
func (s *Store) DeleteCall(projectID, serviceID, opID, callID string) error {
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		pi, si, oi, ok := locateOperation(list, projectID, serviceID, opID)
		if !ok {
			return nil, ErrNotFound
		}
		op := &list[pi].Services[si].Operations[oi]
		for ci := range op.Calls {
			if op.Calls[ci].ID == callID {
				op.Calls = append(op.Calls[:ci], op.Calls[ci+1:]...)
				return list, nil
			}
		}
		return nil, ErrNotFound
	})
}

// DuplicateCall copies a call within its operation, giving the copy a title
// that does not collide with a sibling ("查询" → "查询 (副本)" → "查询 (副本 2)").
// The UI uses it to create a variant of an existing sample instead of starting
// from the operation template.
func (s *Store) DuplicateCall(projectID, serviceID, opID, callID string) (model.Call, error) {
	var out model.Call
	err := s.commit(func(list []model.Project) ([]model.Project, error) {
		pi, si, oi, ok := locateOperation(list, projectID, serviceID, opID)
		if !ok {
			return nil, ErrNotFound
		}
		op := &list[pi].Services[si].Operations[oi]
		for _, c := range op.Calls {
			if c.ID != callID {
				continue
			}
			c.ID = NewID()
			c.Title = copyTitle(c.Title, op.Calls)
			c.Headers = append([]model.Header(nil), c.Headers...)
			c.Response = cloneSavedResponse(c.Response)
			op.Calls = append(op.Calls, c)
			out = c
			return list, nil
		}
		return nil, ErrNotFound
	})
	return out, err
}

// copyTitle returns "<title> (副本)" or, when that is taken, the first free
// "<title> (副本 N)" with N starting at 2.
func copyTitle(title string, siblings []model.Call) string {
	if title == "" {
		title = "请求"
	}
	base := title + " (副本)"
	taken := make(map[string]bool, len(siblings))
	for _, c := range siblings {
		taken[c.Title] = true
	}
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (副本 %d)", title, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// EnsureIDs fills in missing ids throughout a project tree.
func EnsureIDs(p *model.Project) {
	if p.ID == "" {
		p.ID = NewID()
	}
	for i := range p.Services {
		if p.Services[i].ID == "" {
			p.Services[i].ID = NewID()
		}
		for j := range p.Services[i].Operations {
			assignOperationIDs(&p.Services[i].Operations[j])
		}
	}
}

func assignOperationIDs(op *model.Operation) {
	if op.ID == "" {
		op.ID = NewID()
	}
	for i := range op.Calls {
		if op.Calls[i].ID == "" {
			op.Calls[i].ID = NewID()
		}
	}
}
