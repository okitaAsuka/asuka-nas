package nas

import (
	"errors"
	"os"
	"path/filepath"
)

// OpenLockedStore 所有产品写入口共用：锁失败前绝不打开/迁移 SQLite。
// 仅准备私有父目录；调用者按 store、lock 顺序关闭。
func OpenLockedStore(database string) (*Store, *os.File, error) {
	if err := os.MkdirAll(filepath.Dir(database), 0700); err != nil {
		return nil, nil, errors.New("database_directory")
	}
	lock, err := AcquireInstance(database)
	if err != nil {
		return nil, nil, err
	}
	s, err := OpenStore(database)
	if err != nil {
		lock.Close()
		return nil, nil, err
	}
	return s, lock, nil
}
