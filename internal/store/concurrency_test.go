package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"soapweb/internal/model"
)

// newStoreWithSample builds a store holding one project with one service, one
// operation and one call.
func newStoreWithSample(t *testing.T) (*Store, model.Project) {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	saved, err := s.AddProject(model.Project{
		Name: "P",
		Services: []model.Service{{
			ID:   "s1",
			Name: "S",
			Operations: []model.Operation{{
				ID:       "o1",
				Name:     "O",
				Template: "<a/>",
				Calls:    []model.Call{{ID: "c1", Title: "first", Request: "<x/>"}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	return s, saved
}

// TestListReturnsDeepCopy 回归：List 曾只做 copy(out, s.projects)，
// 内层 Services/Operations/Calls 仍与 store 共享底层数组，调用方在锁外
// 遍历时会与并发写入构成 data race（-race 可捕获）。
func TestListReturnsDeepCopy(t *testing.T) {
	s, project := newStoreWithSample(t)

	list := s.List()
	// 篡改返回值的每一层，store 内部状态都必须不受影响。
	list[0].Name = "mutated"
	list[0].Services[0].Name = "mutated"
	list[0].Services[0].Operations[0].Name = "mutated"
	list[0].Services[0].Operations[0].Calls[0].Title = "mutated"

	got, err := s.Get(project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "P" {
		t.Errorf("project name = %q，返回值的修改泄漏到了 store", got.Name)
	}
	if got.Services[0].Name != "S" {
		t.Errorf("service name = %q，返回值的修改泄漏到了 store", got.Services[0].Name)
	}
	if got.Services[0].Operations[0].Name != "O" {
		t.Errorf("operation name = %q，返回值的修改泄漏到了 store", got.Services[0].Operations[0].Name)
	}
	if got.Services[0].Operations[0].Calls[0].Title != "first" {
		t.Errorf("call title = %q，返回值的修改泄漏到了 store", got.Services[0].Operations[0].Calls[0].Title)
	}
}

// TestGetReturnsDeepCopy 同 List，针对单对象读取路径。
func TestGetReturnsDeepCopy(t *testing.T) {
	s, project := newStoreWithSample(t)

	got, err := s.Get(project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got.Services[0].Operations[0].Calls[0].Request = "mutated"
	got.Services[0].Operations[0].Calls = nil
	got.Services[0].Operations[0].Template = "mutated"

	again, err := s.Get(project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	op := again.Services[0].Operations[0]
	if op.Template != "<a/>" {
		t.Errorf("template = %q，修改泄漏到了 store", op.Template)
	}
	if len(op.Calls) != 1 || op.Calls[0].Request != "<x/>" {
		t.Errorf("calls = %+v，修改泄漏到了 store", op.Calls)
	}
}

// TestConcurrentListAndWriteHasNoRace 是原缺陷的直接复现场景：
// 一边持续写入（append 到 Calls），一边在锁外序列化 List 的返回值。
//
// 说明：这个测试在「浅拷贝 + 原地修改」的实现下会让 -race 报警。改为
// commit()（克隆→落盘→换入）之后，已发布的状态不再被原地修改，即使
// List 返回浅拷贝也不会竞争 —— 也就是说本测试如今守的是 commit 的
// copy-on-write 不变量，而「返回值是否可被调用方篡改」由
// TestListReturnsDeepCopy / TestGetReturnsDeepCopy 负责。两者都需要，
// 因为深拷贝对调用方契约和并发安全是两道独立的保障。
func TestConcurrentListAndWriteHasNoRace(t *testing.T) {
	s, project := newStoreWithSample(t)

	var wg sync.WaitGroup
	done := make(chan struct{})

	// 写者：不断新增与删除 call。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			_, err := s.AddCall(project.ID, "s1", "o1", model.Call{Title: "c", Request: "<x/>"})
			if err != nil {
				return
			}
			calls := s.projects[0].Services[0].Operations[0].Calls
			if len(calls) > 3 {
				_ = s.DeleteCall(project.ID, "s1", "o1", calls[0].ID)
			}
		}
	}()

	// 读者：模拟 API handler 在锁外把 List 结果编码为 JSON。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			list := s.List()
			for _, p := range list {
				for _, svc := range p.Services {
					for _, op := range svc.Operations {
						for _, c := range op.Calls {
							_, _ = json.Marshal(c)
						}
					}
				}
			}
		}
		close(done)
	}()

	wg.Wait()
}

// TestSaveFailureLeavesStateUntouched 回归：AddProject 曾先 append 再 save，
// 失败不回滚 —— 内存里有、磁盘上没有，API 已返回 500 但对象仍在服务端，
// 重启后凭空消失。现在失败必须整体不生效。
func TestSaveFailureLeavesStateUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := s.AddProject(model.Project{Name: "OK"}); err != nil {
		t.Fatalf("首次 AddProject: %v", err)
	}

	// 把临时文件路径占成一个目录，使后续写入必定失败。
	tmp := path + ".tmp"
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Skipf("无法制造写入失败: %v", err)
	}

	if _, err := s.AddProject(model.Project{Name: "SHOULD_FAIL"}); err == nil {
		t.Fatal("写入失败时应返回错误")
	}

	mem := s.List()
	if len(mem) != 1 || mem[0].Name != "OK" {
		t.Fatalf("写入失败后内存被污染: %+v", mem)
	}

	disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取数据文件: %v", err)
	}
	var onDisk []model.Project
	if err := json.Unmarshal(disk, &onDisk); err != nil {
		t.Fatalf("解析数据文件: %v", err)
	}
	if len(onDisk) != len(mem) {
		t.Fatalf("内存 %d 个工程、磁盘 %d 个：状态分叉", len(mem), len(onDisk))
	}
}

// TestSaveFailureOnUpdateKeepsOldValue 更新路径同样不应半途生效。
func TestSaveFailureOnUpdateKeepsOldValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	saved, err := s.AddProject(model.Project{Name: "old"})
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	tmp := path + ".tmp"
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Skipf("无法制造写入失败: %v", err)
	}

	updated := saved
	updated.Name = "new"
	if err := s.UpdateProject(updated); err == nil {
		t.Fatal("写入失败时应返回错误")
	}

	got, err := s.Get(saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "old" {
		t.Fatalf("更新失败后内存已变更: name = %q", got.Name)
	}
}

// TestNotFoundDoesNotTouchDisk 找不到目标时不应写盘。
func TestNotFoundDoesNotTouchDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := s.AddProject(model.Project{Name: "P"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if err := s.DeleteProject("missing"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("未找到对象时不应重写数据文件")
	}
}

// TestProxyDeepCopy 确认 *ProxySettings 也被深拷贝（共享指针同样危险）。
func TestProxyDeepCopy(t *testing.T) {
	s, project := newStoreWithSample(t)
	proxy := &model.ProxySettings{Mode: "custom", Type: "http", Host: "127.0.0.1", Port: 8080}
	p := project
	p.Proxy = proxy
	if err := s.UpdateProject(p); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	got, err := s.Get(project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Proxy == nil {
		t.Fatal("proxy 丢失")
	}
	got.Proxy.Host = "mutated"

	again, err := s.Get(project.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if again.Proxy.Host != "127.0.0.1" {
		t.Errorf("proxy host = %q，修改泄漏到了 store", again.Proxy.Host)
	}
}
