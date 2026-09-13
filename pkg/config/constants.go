package config

// 系统专属保留舱位 ID 与名称常量
const (
	// DefaultRootFilesID 根目录同级文件归集舱位的系统专属 UUID (天然全球唯一，直接作为舱位 ID 与集装箱文件名称，杜绝任何文件夹冲突)
	DefaultRootFilesID = "71c038f0-c62e-457b-9768-95b72e004fd4"

	// RootFilesCargoName 根级同级文件归集舱位展示名称
	RootFilesCargoName = "Root Files (根级配置与同级文件)"
)

// 默认配置参数常量
const (
	DefaultCategory       = "vps"
	DefaultRetentionCount = 5
	DefaultPushRetry      = 3
	DefaultTagPrecision   = "second"
	DefaultRepository     = "ghcr.io/duorameng/ark"
	DefaultEngine         = "oci"

	// 舱位优先级默认值 (数值越小排越前，越先构建作为基础缓存层)
	DefaultPriorityRootFiles = 10 // 根级文件变动频率极低，作为首层高速缓存
	DefaultPriorityBase      = 30 // 普通业务目录默认优先级
	DefaultPriorityDatabase  = 70 // 数据库/易变数据舱位排在靠后层
)

// 关键配置文件与系统目录名称
const (
	ConfigFileName  = "config.json"
	EnvFileName     = ".env"
	CacheDirName    = "cache"
	TmpDirName      = "tmp"
	SealKeyFileName = "seal.key"
	CargoOCIPath    = "/cargo" // OCI 容器内货物挂载层前缀
)

// 环境变量名称集中定义
const (
	EnvArkRepository        = "ARK_REPOSITORY"
	EnvArkImage             = "ARK_IMAGE"
	EnvArkCategory          = "ARK_CATEGORY"
	EnvArkSealKey           = "ARK_SEAL_KEY"
	EnvArkEngine            = "ARK_ENGINE"
	EnvArkPushRetry         = "ARK_PUSH_RETRY"
	EnvArkCleanAfterPush    = "ARK_CLEAN_AFTER_PUSH"
	EnvArkCleanAllAfterPush = "ARK_CLEAN_ALL_AFTER_PUSH"
	EnvArkRetentionCount    = "ARK_RETENTION_COUNT"
	EnvArkBackupDir         = "ARK_BACKUP_DIR"
	EnvArkBackupPath        = "ARK_BACKUP_PATH"
	EnvArkSourceDir         = "ARK_SOURCE_DIR"
	EnvArkDestDir           = "ARK_DEST_DIR"
	EnvArkDeployDir         = "ARK_DEPLOY_DIR"
	EnvArkRestoreDir        = "ARK_RESTORE_DIR"
	EnvArkProxy             = "ARK_PROXY"
	EnvArkTarget            = "ARK_TARGET"
	EnvArkUsername          = "ARK_USERNAME"
	EnvArkPassword          = "ARK_PASSWORD"
	EnvArkToken             = "ARK_TOKEN"
	EnvArkTag               = "ARK_TAG"
	EnvArkFixedTag          = "ARK_FIXED_TAG"
	EnvAliyunRepository     = "ALIYUN_REPOSITORY"
	EnvAliyunUsername       = "ALIYUN_USERNAME"
	EnvAliyunPassword       = "ALIYUN_PASSWORD"
	EnvGithubRepository     = "GITHUB_REPOSITORY"
	EnvGhToken              = "GH_TOKEN"
	EnvGithubToken          = "GITHUB_TOKEN"
)

// 归档文件后缀常量
const (
	ExtDat    = ".dat"
	ExtTar    = ".tar"
	ExtTarGz  = ".tar.gz"
	ExtTgz    = ".tgz"
	ExtEnc    = ".enc"
	ExtTarEnc = ".tar.enc"
)
