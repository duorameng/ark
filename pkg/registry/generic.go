package registry

// GenericOCIProvider 实现通用第三方 OCI 注册表流程 (如 Harbor, Docker Hub, 私有 Registry 等)。
// 直接继承 BaseProvider 的标准通用 OCI 行为契约，零样板冗余代码。
type GenericOCIProvider struct {
	*BaseProvider
}

// NewGenericOCIProvider 构造通用 OCI Provider
func NewGenericOCIProvider(repository, username, password string) *GenericOCIProvider {
	base := NewBaseProvider("generic", "通用 OCI 镜像注册表", repository, username, password)
	return &GenericOCIProvider{
		BaseProvider: base,
	}
}
