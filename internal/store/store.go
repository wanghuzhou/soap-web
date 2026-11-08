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
	mu       sync.RWMutex
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
				body := op.Template
				if body == "" {
					body = op.Request
				}
				if body == "" {
					continue
				}
				title := op.Name
				if title == "" {
					title = "请求"
				}
				op.Calls = []model.Call{{
					ID:       NewID(),
					Title:    title,
					Endpoint: op.Endpoint,
					Action:   op.SoapAction,
					Request:  body,
				}}
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

// saveProjects persists the project list atomically. Caller must hold the lock.
func (s *Store) saveProjects(projects []model.Project) error {
	return fileutil.WriteJSONAtomic(s.path, projects, 0o644)
}

// commit applies mutate to a deep copy, persists it, and only then swaps it
// into memory, so a failed write leaves memory unchanged. Caller holds the lock.
func (s *Store) commit(mutate func([]model.Project) ([]model.Project, error)) error {
	next := cloneProjects(s.projects)
	next, err := mutate(next)
	if err != nil {
		return err
	}
	if err := s.saveProjects(next); err != nil {
		return err
	}
	s.projects = next
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
		copy(out.Calls, op.Calls)
	}
	return out
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
	for _, p := range s.projects {
		if p.ID == id {
			return cloneProject(p), nil
		}
	}
	return model.Project{}, ErrNotFound
}

// ---- project-level CRUD ----

// AddProject appends a project, assigning an id if missing, and saves.
func (s *Store) AddProject(p model.Project) (model.Project, error) {
	if err := p.Proxy.Validate(); err != nil {
		return p, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		pi := indexOfProject(list, projectID)
		if pi < 0 {
			return nil, ErrNotFound
		}
		for i := range list[pi].Services {
			if list[pi].Services[i].ID == serviceID {
				list[pi].Services = append(list[pi].Services[:i], list[pi].Services[i+1:]...)
				return list, nil
			}
		}
		return nil, ErrNotFound
	})
}

// AddOperation appends an operation to a service.
func (s *Store) AddOperation(projectID, serviceID string, op model.Operation) (model.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	assignOperationIDs(&op)
	err := s.commit(func(list []model.Project) ([]model.Project, error) {
		pi := indexOfProject(list, projectID)
		if pi < 0 {
			return nil, ErrNotFound
		}
		for si := range list[pi].Services {
			if list[pi].Services[si].ID != serviceID {
				continue
			}
			list[pi].Services[si].Operations = append(list[pi].Services[si].Operations, cloneOperation(op))
			return list, nil
		}
		return nil, ErrNotFound
	})
	return op, err
}

// DeleteOperation removes an operation from a service.
func (s *Store) DeleteOperation(projectID, serviceID, opID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		pi := indexOfProject(list, projectID)
		if pi < 0 {
			return nil, ErrNotFound
		}
		for si := range list[pi].Services {
			svc := &list[pi].Services[si]
			if svc.ID != serviceID {
				continue
			}
			for oi := range svc.Operations {
				if svc.Operations[oi].ID == opID {
					svc.Operations = append(svc.Operations[:oi], svc.Operations[oi+1:]...)
					return list, nil
				}
			}
		}
		return nil, ErrNotFound
	})
}

// AddCall appends a request sample (call) to an operation.
func (s *Store) AddCall(projectID, serviceID, opID string, call model.Call) (model.Call, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if call.ID == "" {
		call.ID = NewID()
	}
	if call.Title == "" {
		call.Title = "新请求 " + time.Now().Format("15:04:05")
	}
	err := s.commit(func(list []model.Project) ([]model.Project, error) {
		pi := indexOfProject(list, projectID)
		if pi < 0 {
			return nil, ErrNotFound
		}
		for si := range list[pi].Services {
			svc := &list[pi].Services[si]
			if svc.ID != serviceID {
				continue
			}
			for oi := range svc.Operations {
				op := &svc.Operations[oi]
				if op.ID != opID {
					continue
				}
				// Seed the new request from the operation's structural template
				// (方案 A: pure skeleton from WSDL, no business data). Fall back
				// to op.Request only when no Template exists.
				if call.Request == "" {
					call.Request = op.Template
					if call.Request == "" {
						call.Request = op.Request
					}
					if call.Request == "" && len(op.Calls) > 0 {
						call.Request = op.Calls[0].Request
					}
				}
				if call.Endpoint == "" {
					call.Endpoint = op.Endpoint
				}
				if call.Action == "" {
					call.Action = op.SoapAction
				}
				op.Calls = append(op.Calls, call)
				return list, nil
			}
		}
		return nil, ErrNotFound
	})
	return call, err
}

// UpdateCall replaces a call (by id) within an operation.
func (s *Store) UpdateCall(projectID, serviceID, opID string, call model.Call) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		pi := indexOfProject(list, projectID)
		if pi < 0 {
			return nil, ErrNotFound
		}
		for si := range list[pi].Services {
			svc := &list[pi].Services[si]
			if svc.ID != serviceID {
				continue
			}
			for oi := range svc.Operations {
				op := &svc.Operations[oi]
				if op.ID != opID {
					continue
				}
				for ci := range op.Calls {
					if op.Calls[ci].ID == call.ID {
						op.Calls[ci] = call
						return list, nil
					}
				}
				return nil, ErrNotFound
			}
		}
		return nil, ErrNotFound
	})
}

// DeleteCall removes a call from an operation.
func (s *Store) DeleteCall(projectID, serviceID, opID, callID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit(func(list []model.Project) ([]model.Project, error) {
		pi := indexOfProject(list, projectID)
		if pi < 0 {
			return nil, ErrNotFound
		}
		for si := range list[pi].Services {
			svc := &list[pi].Services[si]
			if svc.ID != serviceID {
				continue
			}
			for oi := range svc.Operations {
				op := &svc.Operations[oi]
				if op.ID != opID {
					continue
				}
				for ci := range op.Calls {
					if op.Calls[ci].ID == callID {
						op.Calls = append(op.Calls[:ci], op.Calls[ci+1:]...)
						return list, nil
					}
				}
				return nil, ErrNotFound
			}
		}
		return nil, ErrNotFound
	})
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
