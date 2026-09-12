package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"ark/pkg/archive"
)

// DirInfo 包含目录哈希特征与统计信息
type DirInfo struct {
	Hash       string
	FileCount  int
	TotalBytes int64
}

type fileTask struct {
	fullPath string
	relPath  string
	size     int64
}

type fileResult struct {
	entry string // 格式: relPath|size|sha256
	size  int64
	err   error
}

// ComputeDirTreeHash 并发计算目录的确定性哈希树 (Tree Hash)
func ComputeDirTreeHash(dirPath string) (*DirInfo, error) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s 不是一个有效目录", dirPath)
	}

	tasks := make([]fileTask, 0, 1024)
	err = filepath.Walk(dirPath, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过无法读取的特殊文件
		}
		if f.IsDir() {
			if archive.ShouldIgnoreDir(f.Name()) && path != dirPath {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return nil
		}
		// 统一路径分隔符为 /
		rel = filepath.ToSlash(rel)
		tasks = append(tasks, fileTask{
			fullPath: path,
			relPath:  rel,
			size:     f.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(tasks) == 0 {
		return &DirInfo{Hash: "empty", FileCount: 0, TotalBytes: 0}, nil
	}

	numWorkers := runtime.NumCPU() * 2
	if numWorkers < 4 {
		numWorkers = 4
	}
	if numWorkers > len(tasks) {
		numWorkers = len(tasks)
	}

	taskCh := make(chan fileTask, len(tasks))
	resCh := make(chan fileResult, len(tasks))
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskCh {
				h := sha256.New()
				f, err := os.Open(t.fullPath)
				if err != nil {
					resCh <- fileResult{err: err}
					continue
				}
				_, err = io.Copy(h, f)
				f.Close()
				if err != nil {
					resCh <- fileResult{err: err}
					continue
				}

				sum := hex.EncodeToString(h.Sum(nil))
				resCh <- fileResult{
					entry: fmt.Sprintf("%s|%d|%s", t.relPath, t.size, sum),
					size:  t.size,
				}
			}
		}()
	}

	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	wg.Wait()
	close(resCh)

	entries := make([]string, 0, len(tasks))
	var totalBytes int64
	for r := range resCh {
		if r.err != nil {
			continue
		}
		entries = append(entries, r.entry)
		totalBytes += r.size
	}

	// 严格按相对路径排序，保证确定性
	sort.Strings(entries)

	treeHasher := sha256.New()
	for _, entry := range entries {
		treeHasher.Write([]byte(entry + "\n"))
	}

	return &DirInfo{
		Hash:       hex.EncodeToString(treeHasher.Sum(nil)),
		FileCount:  len(entries),
		TotalBytes: totalBytes,
	}, nil
}
