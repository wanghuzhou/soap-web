package store

import (
	"path/filepath"
	"testing"

	"soapweb/internal/model"
)

// TestDeepCopyCoversTestSuites 深拷贝必须覆盖 TestSuites/TestCases/Steps，
// 否则从 SoapUI 导入的测试套件仍与 store 共享内存。
func TestDeepCopyCoversTestSuites(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	saved, err := s.AddProject(model.Project{
		Name: "P",
		TestSuites: []model.TestSuite{{
			Name: "TS",
			TestCases: []model.TestCase{{
				Name:  "TC",
				Steps: []model.TestStep{{Name: "step", Type: "request", Request: "<x/>"}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	list := s.List()
	list[0].TestSuites[0].Name = "mutated"
	list[0].TestSuites[0].TestCases[0].Name = "mutated"
	list[0].TestSuites[0].TestCases[0].Steps[0].Request = "mutated"

	got, err := s.Get(saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TestSuites[0].Name != "TS" {
		t.Errorf("testsuite name = %q", got.TestSuites[0].Name)
	}
	if got.TestSuites[0].TestCases[0].Name != "TC" {
		t.Errorf("testcase name = %q", got.TestSuites[0].TestCases[0].Name)
	}
	if got.TestSuites[0].TestCases[0].Steps[0].Request != "<x/>" {
		t.Errorf("step request = %q", got.TestSuites[0].TestCases[0].Steps[0].Request)
	}
}

// TestNilSlicesSurviveClone 空 slice 的深拷贝不应 panic 或变成非 nil。
func TestNilSlicesSurviveClone(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 没有任何 Services / TestSuites / Proxy 的最小工程。
	if _, err := s.AddProject(model.Project{Name: "bare"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("list = %d", len(list))
	}
	if list[0].Services != nil {
		t.Errorf("Services 应为 nil，实际 %+v", list[0].Services)
	}
	if list[0].TestSuites != nil {
		t.Errorf("TestSuites 应为 nil，实际 %+v", list[0].TestSuites)
	}
	if list[0].Proxy != nil {
		t.Errorf("Proxy 应为 nil")
	}
}

// TestUpdateProjectProxyOnly 窄接口只改 proxy，其他字段不变。
func TestUpdateProjectProxyOnly(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	saved, err := s.AddProject(model.Project{
		Name: "P",
		Services: []model.Service{{
			ID: "s1", Name: "S",
			Operations: []model.Operation{{ID: "o1", Name: "O", Template: "<a/>"}},
		}},
	})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	proxy := &model.ProxySettings{Mode: "custom", Type: "socks5", Host: "10.0.0.1", Port: 1080}
	if err := s.UpdateProjectProxy(saved.ID, proxy); err != nil {
		t.Fatalf("UpdateProjectProxy: %v", err)
	}

	got, err := s.Get(saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "P" {
		t.Errorf("name 被改动: %q", got.Name)
	}
	if len(got.Services) != 1 || got.Services[0].Operations[0].Template != "<a/>" {
		t.Errorf("工程结构被改动: %+v", got.Services)
	}
	if got.Proxy == nil || got.Proxy.Host != "10.0.0.1" || got.Proxy.Type != "socks5" {
		t.Errorf("proxy = %+v", got.Proxy)
	}

	// 传 nil 表示清除代理。
	if err := s.UpdateProjectProxy(saved.ID, nil); err != nil {
		t.Fatalf("清除代理失败: %v", err)
	}
	again, _ := s.Get(saved.ID)
	if again.Proxy != nil {
		t.Errorf("清除后 proxy 仍存在: %+v", again.Proxy)
	}
}

// TestUpdateProjectProxyValidation 非法配置必须被拒绝且不落盘。
func TestUpdateProjectProxyValidation(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	saved, err := s.AddProject(model.Project{Name: "P"})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	bad := &model.ProxySettings{Mode: "custom", Type: "http", Host: "", Port: 8080}
	if err := s.UpdateProjectProxy(saved.ID, bad); err == nil {
		t.Fatal("无效配置应返回错误")
	}
	got, _ := s.Get(saved.ID)
	if got.Proxy != nil {
		t.Errorf("无效配置不应被保存: %+v", got.Proxy)
	}
}

// TestRenameProjectOnly 重命名只改 name。
func TestRenameProjectOnly(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	saved, err := s.AddProject(model.Project{
		Name: "old",
		Services: []model.Service{{
			ID: "s1", Name: "S",
			Operations: []model.Operation{{ID: "o1", Name: "O", Calls: []model.Call{{ID: "c1"}}}},
		}},
	})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	if err := s.RenameProject(saved.ID, "new"); err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	got, _ := s.Get(saved.ID)
	if got.Name != "new" {
		t.Errorf("name = %q", got.Name)
	}
	if len(got.Services[0].Operations[0].Calls) != 1 {
		t.Errorf("重命名破坏了工程结构")
	}

	if err := s.RenameProject("missing", "x"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestListEmptyStore 空 store 的深拷贝。
func TestListEmptyStore(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if list := s.List(); len(list) != 0 {
		t.Fatalf("空 store 应返回 0 个工程，实际 %d", len(list))
	}
	if _, err := s.Get("missing"); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestCallHeadersAreNotShared guards AddCall/UpdateCall: both used to store the
// caller's Call value directly, so mutating its Headers slice afterwards
// changed the store without going through a write. The package promises deep
// copies in both directions.
func TestCallHeadersAreNotShared(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p, err := s.AddProject(model.Project{Name: "P"})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	svc, err := s.AddService(p.ID, model.Service{Name: "S"})
	if err != nil {
		t.Fatalf("AddService: %v", err)
	}
	op, err := s.AddOperation(p.ID, svc.ID, model.Operation{Name: "O"})
	if err != nil {
		t.Fatalf("AddOperation: %v", err)
	}

	added, err := s.AddCall(p.ID, svc.ID, op.ID, model.Call{
		Title:   "c",
		Headers: []model.Header{{Name: "X-Token", Value: "original"}},
	})
	if err != nil {
		t.Fatalf("AddCall: %v", err)
	}
	added.Headers[0].Value = "mutated-add"
	if got := storedHeaders(t, s, p.ID); got[0].Value != "original" {
		t.Fatalf("AddCall shares the caller's header slice: %q", got[0].Value)
	}

	// Update path: the value read back is itself a copy; mutating the struct we
	// pass to UpdateCall must not affect the stored sample afterwards.
	updated, err := s.Get(p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	call := updated.Services[0].Operations[0].Calls[0]
	call.Headers = []model.Header{{Name: "X-Token", Value: "updated"}}
	if err := s.UpdateCall(p.ID, svc.ID, op.ID, call); err != nil {
		t.Fatalf("UpdateCall: %v", err)
	}
	call.Headers[0].Value = "mutated-update"
	if got := storedHeaders(t, s, p.ID); got[0].Value != "updated" {
		t.Fatalf("UpdateCall shares the caller's header slice: %q", got[0].Value)
	}
}

func storedHeaders(t *testing.T, s *Store, projectID string) []model.Header {
	t.Helper()
	got, err := s.Get(projectID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return got.Services[0].Operations[0].Calls[0].Headers
}
