package archive

import (
	"strings"

	"ark/pkg/config"
)

// SupportedArchiveExts 系统支持的所有货物归档扩展名
var SupportedArchiveExts = []string{
	config.ExtDat,
	config.ExtTar,
	config.ExtTarGz,
	config.ExtTgz,
	config.ExtEnc,
	config.ExtTarEnc,
}

// EncryptedArchiveExts 包含 AES-256 加密封条的货物扩展名
var EncryptedArchiveExts = []string{
	config.ExtDat,
	config.ExtEnc,
	config.ExtTarEnc,
}

// IsSupportedArchive 判断文件是否为系统可识别的货物包
func IsSupportedArchive(filename string) bool {
	lower := strings.ToLower(filename)
	for _, ext := range SupportedArchiveExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// IsEncryptedArchive 判断货物包是否需要密钥开封
func IsEncryptedArchive(filename string) bool {
	lower := strings.ToLower(filename)
	for _, ext := range EncryptedArchiveExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
