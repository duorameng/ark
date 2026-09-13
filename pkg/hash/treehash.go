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
	modTime  int64
	mode     uint32
}

type fileResult struct {
	entry string
	size  int64
	err   error
}

// ComputeDirTreeHash 兼容旧接口，计算普通目录的 Tree Hash
func ComputeDirTreeHash(dirPath string) (*DirInfo, error) {
	return ComputeSourceTreeHash(dirPath, false)
}

// computeFastSampleHash 计算单文件的极速指纹特征
// 采用 "纳秒级修改时间 + 精确尺寸 + 权限属性 + 首尾内容采样 (若>64KB)" 机制
// 既保持了纳秒级的变动敏锐度，又彻底避免对大文件进行全盘无意义读取，实现微秒级瞬间检视
func computeFastSampleHash(fullPath string, size int64, modTimeNano int64, mode uint32) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|%d|", size, modTimeNano, mode)

	if size <= 0 {
		return hex.EncodeToString(h.Sum(nil)), nil
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if size <= 64*1024 {
		// 小文件 (<=64KB): 全量读取内容特征
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
	} else {
		// 大文件 (>64KB): 快速采样头部 4KB + 尾部 4KB
		headBuf := make([]byte, 4096)
		n, _ := io.ReadFull(f, headBuf)
		h.Write(headBuf[:n])

		tailBuf := make([]byte, 4096)
		tailOffset := size - 4096
		if tailOffset < 0 {
			tailOffset = 0
		}
		_, _ = f.Seek(tailOffset, io.SeekStart)
		n, _ = io.ReadFull(f, tailBuf)
		h.Write(tailBuf[:n])
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ComputeSourceTreeHash 并发计算目录或同级文件的确定性极速哈希树 (Fast Tree Hash)
func ComputeSourceTreeHash(dirPath string, filesOnly bool) (*DirInfo, error) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		// 单文件极速指纹计算
		fastHash, err := computeFastSampleHash(dirPath, info.Size(), info.ModTime().UnixNano(), uint32(info.Mode()))
		if err != nil {
			return nil, err
		}
		return &DirInfo{
			Hash:       fastHash,
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
				modTime:  fi.ModTime().UnixNano(),
				mode:     uint32(fi.Mode()),
			})
		}
	} else {
		// 完整递归遍历子目录
		err = filepath.Walk(dirPath, func(path string, f os.FileInfo, err error) error {
			if err != nil {
				return nil
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
			rel = filepath.ToSlash(rel)
			tasks = append(tasks, fileTask{
				fullPath: path,
				relPath:  rel,
				size:     f.Size(),
				modTime:  f.ModTime().UnixNano(),
				mode:     uint32(f.Mode()),
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
				sum, err := computeFastSampleHash(t.fullPath, t.size, t.modTime, t.mode)
				if err != nil {
					resCh <- fileResult{err: err}
					continue
				}
				resCh <- fileResult{
					entry: fmt.Sprintf("%s|%d|%d|%s", t.relPath, t.size, t.modTime, sum),
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
