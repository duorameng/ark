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
	"strings"
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

// ComputeDirTreeHash 兼容旧接口，计算普通目录的 Tree Hash
func ComputeDirTreeHash(dirPath string) (*DirInfo, error) {
	return ComputeSourceTreeHash(dirPath, false)
}

// ComputeSourceTreeHash 并发计算目录或同级文件的确定性哈希树 (Tree Hash)
func ComputeSourceTreeHash(dirPath string, filesOnly bool) (*DirInfo, error) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		// 单文件直接计算哈希
		f, err := os.Open(dirPath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return nil, err
		}
		return &DirInfo{
			Hash:       hex.EncodeToString(h.Sum(nil)),
			FileCount:  1,
			TotalBytes: info.Size(),
		}, nil
	}

	tasks := make([]fileTask, 0, 1024)

	if filesOnly {
		// 仅归集该目录下的直接同级文件 (不递归子目录)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if name == "ark" || name == "ark.exe" || name == "tmp" || name == "cache" || strings.HasPrefix(name, ".git") {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			tasks = append(tasks, fileTask{
				fullPath: filepath.Join(dirPath, name),
				relPath:  name,
				size:     fi.Size(),
			})
		}
	} else {
		// 完整递归遍历子目录
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
