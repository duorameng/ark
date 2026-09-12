//go:build linux

package archive

import (
	"archive/tar"
	"os"
	"syscall"
)

func fillOSMetadata(fi os.FileInfo, hdr *tar.Header) {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		hdr.Uid = int(stat.Uid)
		hdr.Gid = int(stat.Gid)
	}
}

func restoreOwnership(target string, uid, gid int) {
	if uid >= 0 && gid >= 0 {
		_ = os.Chown(target, uid, gid)
	}
}

func restoreSymlinkOwnership(target string, uid, gid int) {
	if uid >= 0 && gid >= 0 {
		_ = os.Lchown(target, uid, gid)
	}
}
