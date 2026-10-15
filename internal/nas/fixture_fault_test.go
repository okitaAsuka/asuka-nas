package nas

import (
	"fmt"
	"regexp"
	"sync/atomic"
)

var fixtureEventKind = regexp.MustCompile(`^[a-z][a-z_]{0,63}$`)
var fixtureFaultSequence atomic.Uint64

// failEvent 只向临时数据库注入事件提交故障；返回幂等解除函数，重开后仍作用于同一库。
// 可选序号限定一个文件，保留其它文件已提交的恢复点；不用任意 SQL 片段配置故障。
func (f *fixture) failEvent(kind string, fileIndex ...int) func() {
	f.t.Helper()
	if !fixtureEventKind.MatchString(kind) || len(fileIndex) > 1 || len(fileIndex) == 1 && fileIndex[0] < 0 {
		f.t.Fatal("invalid fixture event fault")
	}
	name := fmt.Sprintf("fixture_event_fault_%d", fixtureFaultSequence.Add(1))
	condition := "NEW.kind='" + kind + "'"
	if len(fileIndex) == 1 {
		condition += fmt.Sprintf(" AND NEW.file_index=%d", fileIndex[0])
	}
	if _, err := f.store.db.Exec("CREATE TRIGGER " + name + " BEFORE INSERT ON events WHEN " + condition + " BEGIN SELECT RAISE(ABORT,'fixture_failure'); END;"); err != nil {
		f.t.Fatal(err)
	}
	active := true
	clear := func() {
		f.t.Helper()
		if !active || f.store == nil {
			return
		}
		if _, err := f.store.db.Exec("DROP TRIGGER " + name); err != nil {
			f.t.Error(err)
			return
		}
		active = false
	}
	f.t.Cleanup(clear)
	return clear
}
