//go:build !linux

package archive

import (
	"archive/tar"
	"os"
)

func fillOSMetadata(fi os.FileInfo, hdr *tar.Header) {
	hdr.Uid = 0
	hdr.Gid = 0
}

func restoreOwnership(target string, uid, gid int) {
	// 非 Linux 平台安全忽略所有权恢复
}

func restoreSymlinkOwnership(target string, uid, gid int) {
	// 非 Linux 平台安全忽略
}
