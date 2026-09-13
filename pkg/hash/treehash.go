package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ark/pkg/archive"
)

// DirInfo 包含目录哈希特征与统计信息
type DirInfo struct {
	Hash       string
	FileCount  int
	TotalBytes int64
}

// PathFilter 抽象路径过滤匹配器接口
type PathFilter interface {
	ShouldIgnore(fullPath, cargoRoot string, isDir bool) bool
}

// ComputeDirTreeHash 兼容旧接口，计算普通目录的 Tree Hash
func ComputeDirTreeHash(dirPath string) (*DirInfo, error) {
	return ComputeSourceTreeHashWithFilter(dirPath, false, nil)
}

// ComputeSourceTreeHash 基于极速元数据快照计算目录或同级文件的确定性哈希树 (兼容老接口)
func ComputeSourceTreeHash(dirPath string, filesOnly bool) (*DirInfo, error) {
	return ComputeSourceTreeHashWithFilter(dirPath, filesOnly, nil)
}

// ComputeSourceTreeHashWithFilter 基于极速元数据快照计算目录或同级文件的确定性哈希树，支持 PathFilter 排除过滤
func ComputeSourceTreeHashWithFilter(dirPath string, filesOnly bool, filter PathFilter) (*DirInfo, error) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if filter != nil && filter.ShouldIgnore(dirPath, dirPath, false) {
			return &DirInfo{Hash: "empty", FileCount: 0, TotalBytes: 0}, nil
		}
		// 单文件极速元数据指纹
		h := sha256.New()
		fmt.Fprintf(h, "%d|%d|%d", info.Size(), info.ModTime().UnixNano(), uint32(info.Mode()))
		return &DirInfo{
			Hash:       hex.EncodeToString(h.Sum(nil)),
			FileCount:  1,
			TotalBytes: info.Size(),
		}, nil
	}

	entries := make([]string, 0, 1024)
	var totalBytes int64

	if filesOnly {
		// 仅归集该目录下的直接同级文件 (不递归子目录)
		dirEntries, err := os.ReadDir(dirPath)
		if err != nil {
			return nil, err
		}
		for _, e := range dirEntries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if name == "ark" || name == "ark.exe" || name == "tmp" || name == "cache" || strings.HasPrefix(name, ".git") {
				continue
			}
			filePath := filepath.Join(dirPath, name)
			if filter != nil && filter.ShouldIgnore(filePath, dirPath, false) {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			sz := fi.Size()
			totalBytes += sz
			entries = append(entries, fmt.Sprintf("%s|%d|%d|%d", name, sz, fi.ModTime().UnixNano(), uint32(fi.Mode())))
		}
	} else {
		// 使用高效的 WalkDir 遍历整棵目录树，直接利用 DirEntry 避免额外 lstat 系统调用
		err = filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if archive.ShouldIgnoreDir(d.Name()) && path != dirPath {
					return filepath.SkipDir
				}
				if filter != nil && path != dirPath && filter.ShouldIgnore(path, dirPath, true) {
					return filepath.SkipDir
				}
				return nil
			}

			if filter != nil && filter.ShouldIgnore(path, dirPath, false) {
				return nil
			}

			fi, err := d.Info()
			if err != nil {
				return nil
			}

			rel, err := filepath.Rel(dirPath, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)

			sz := fi.Size()
			totalBytes += sz
			entries = append(entries, fmt.Sprintf("%s|%d|%d|%d", rel, sz, fi.ModTime().UnixNano(), uint32(fi.Mode())))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	if len(entries) == 0 {
		return &DirInfo{Hash: "empty", FileCount: 0, TotalBytes: 0}, nil
	}

	// 严格排序保证多平台确定性
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
