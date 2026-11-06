// Package store persists projects to a local JSON file and provides CRUD
// operations over the project -> service -> operation -> call hierarchy.
//
// It is safe for concurrent use. All writes are serialised and persisted
// atomically (write to temp file, then rename) so a crash cannot leave a
// half-written data file.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.projects); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// NewID returns a short random hex identifier.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// save persists the current projects atomically. Caller must hold the lock.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.projects, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns a copy of all projects.
func (s *Store) List() []model.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Project, len(s.projects))
	copy(out, s.projects)
	return out
}

// Get returns one project by id.
func (s *Store) Get(id string) (model.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.projects {
		if p.ID == id {
			return p, nil
		}
	}
	return model.Project{}, ErrNotFound
}

// ---- project-level CRUD ----

// AddProject appends a project, assigning an id if missing, and saves.
func (s *Store) AddProject(p model.Project) (model.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.Name == "" {
		p.Name = "未命名工程"
	}
	assignIDs(&p)
	s.projects = append(s.projects, p)
	return p, s.save()
}

// UpdateProject replaces a whole project (matched by id) and saves.
func (s *Store) UpdateProject(p model.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.projects {
		if s.projects[i].ID == p.ID {
			s.projects[i] = p
			return s.save()
		}
	}
	return ErrNotFound
}

// DeleteProject removes a project by id.
func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.projects {
		if s.projects[i].ID == id {
			s.projects = append(s.projects[:i], s.projects[i+1:]...)
			return s.save()
		}
	}
	return ErrNotFound
}

// ---- nested CRUD helpers ----

// findProject locates a project index. Caller must hold the lock.
func (s *Store) findProject(id string) (int, error) {
	for i := range s.projects {
		if s.projects[i].ID == id {
			return i, nil
		}
	}
	return -1, ErrNotFound
}

// AddService appends a service to a project.
func (s *Store) AddService(projectID string, svc model.Service) (model.Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pi, err := s.findProject(projectID)
	if err != nil {
		return svc, err
	}
	if svc.ID == "" {
		svc.ID = NewID()
	}
	for i := range svc.Operations {
		assignOperationIDs(&svc.Operations[i])
	}
	s.projects[pi].Services = append(s.projects[pi].Services, svc)
	return svc, s.save()
}

// DeleteService removes a service from a project.
func (s *Store) DeleteService(projectID, serviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pi, err := s.findProject(projectID)
	if err != nil {
		return err
	}
	svcs := s.projects[pi].Services
	for i := range svcs {
		if svcs[i].ID == serviceID {
			s.projects[pi].Services = append(svcs[:i], svcs[i+1:]...)
			return s.save()
		}
	}
	return ErrNotFound
}

// AddOperation appends an operation to a service.
func (s *Store) AddOperation(projectID, serviceID string, op model.Operation) (model.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pi, err := s.findProject(projectID)
	if err != nil {
		return op, err
	}
	for si := range s.projects[pi].Services {
		if s.projects[pi].Services[si].ID == serviceID {
			assignOperationIDs(&op)
			s.projects[pi].Services[si].Operations = append(s.projects[pi].Services[si].Operations, op)
			return op, s.save()
		}
	}
	return op, ErrNotFound
}

// DeleteOperation removes an operation from a service.
func (s *Store) DeleteOperation(projectID, serviceID, opID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pi, err := s.findProject(projectID)
	if err != nil {
		return err
	}
	for si := range s.projects[pi].Services {
		svc := &s.projects[pi].Services[si]
		if svc.ID != serviceID {
			continue
		}
		for oi := range svc.Operations {
			if svc.Operations[oi].ID == opID {
				svc.Operations = append(svc.Operations[:oi], svc.Operations[oi+1:]...)
				return s.save()
			}
		}
	}
	return ErrNotFound
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
	pi, err := s.findProject(projectID)
	if err != nil {
		return call, err
	}
	for si := range s.projects[pi].Services {
		svc := &s.projects[pi].Services[si]
		if svc.ID != serviceID {
			continue
		}
		for oi := range svc.Operations {
			if svc.Operations[oi].ID == opID {
				svc.Operations[oi].Calls = append(svc.Operations[oi].Calls, call)
				return call, s.save()
			}
		}
	}
	return call, ErrNotFound
}

// UpdateCall replaces a call (by id) within an operation.
func (s *Store) UpdateCall(projectID, serviceID, opID string, call model.Call) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pi, err := s.findProject(projectID)
	if err != nil {
		return err
	}
	for si := range s.projects[pi].Services {
		svc := &s.projects[pi].Services[si]
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
					return s.save()
				}
			}
			return ErrNotFound
		}
	}
	return ErrNotFound
}

// DeleteCall removes a call from an operation.
func (s *Store) DeleteCall(projectID, serviceID, opID, callID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pi, err := s.findProject(projectID)
	if err != nil {
		return err
	}
	for si := range s.projects[pi].Services {
		svc := &s.projects[pi].Services[si]
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
					return s.save()
				}
			}
			return ErrNotFound
		}
	}
	return ErrNotFound
}

// assignIDs fills in missing ids throughout a project tree.
func assignIDs(p *model.Project) {
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
