package oci

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const (
	MediaTypeDockerManifestV2  = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
	MediaTypeDockerConfigV1    = "application/vnd.docker.container.image.v1+json"
	MediaTypeDockerLayerTar    = "application/vnd.docker.image.rootfs.diff.tar"

	MediaTypeOCIManifestV1 = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeOCIImageIndex = "application/vnd.oci.image.index.v1+json"
	MediaTypeOCIConfigV1   = "application/vnd.oci.image.config.v1+json"
	MediaTypeOCILayerTar   = "application/vnd.oci.image.layer.v1.tar"
)

// Descriptor 标准 OCI / Docker 内容描述符
type Descriptor struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

// Platform 目标运行平台定义
type Platform struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
}

// ManifestDescriptor 用于 Manifest List / Image Index 的平台关联清单描述符
type ManifestDescriptor struct {
	MediaType string   `json:"mediaType"`
	Size      int64    `json:"size"`
	Digest    string   `json:"digest"`
	Platform  Platform `json:"platform"`
}

// ManifestList 多架构镜像清单索引 (Docker Manifest List / OCI Image Index)
type ManifestList struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType,omitempty"`
	Manifests     []ManifestDescriptor `json:"manifests"`
}

// Manifest 单架构 OCI / Docker 镜像清单
type Manifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MediaType     string               `json:"mediaType,omitempty"`
	Config        Descriptor           `json:"config"`
	Layers        []Descriptor         `json:"layers"`
	Manifests     []ManifestDescriptor `json:"manifests,omitempty"` // 兼容解析 Manifest List
}

// ImageConfig 镜像配置结构 (Config JSON)
type ImageConfig struct {
	Architecture string      `json:"architecture"`
	OS           string      `json:"os"`
	Config       ConfigBlock `json:"config"`
	RootFS       RootFSBlock `json:"rootfs"`
	History      []History   `json:"history,omitempty"`
	Created      string      `json:"created,omitempty"`
}

type ConfigBlock struct {
	Cmd          []string            `json:"Cmd,omitempty"`
	Entrypoint   []string            `json:"Entrypoint,omitempty"`
	WorkingDir   string              `json:"WorkingDir,omitempty"`
	Env          []string            `json:"Env,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	StopSignal   string              `json:"StopSignal,omitempty"`
	Labels       map[string]string   `json:"Labels,omitempty"`
}

type RootFSBlock struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

type History struct {
	Created   string `json:"created,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

// GenerateArchConfigJSON 生成指定架构 (amd64 / arm64) 的标准镜像 Config JSON (内置合规微服务元数据伪装)
func GenerateArchConfigJSON(arch string, layers []*TarLayer) ([]byte, string, int64, error) {
	if arch == "" {
		arch = "amd64"
	}

	diffIDs := make([]string, 0, len(layers))
	historyList := make([]History, 0, len(layers))

	epochStr := time.Unix(0, 0).UTC().Format(time.RFC3339)
	for idx, l := range layers {
		digest, err := l.ComputeDigest()
		if err != nil {
			return nil, "", 0, err
		}
		// 由于单文件 Tar 未压缩，uncompressed tar diff_id 严格等于 layer blob sha256
		diffIDs = append(diffIDs, digest)

		var createdBy string
		if idx == 0 && l.TargetCargo == "app/server" {
			createdBy = "COPY server /app/server"
		} else {
			createdBy = fmt.Sprintf("COPY --chown=app:app %s /app/data/", l.FileName)
		}

		historyList = append(historyList, History{
			Created:   epochStr,
			CreatedBy: createdBy,
			Comment:   "build layer",
		})
	}

	cfg := ImageConfig{
		Architecture: arch,
		OS:           "linux",
		Config: ConfigBlock{
			WorkingDir: "/app",
			Cmd:        []string{"/app/server"},
			Env: []string{
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"PORT=8080",
				"GIN_MODE=release",
				"ENVIRONMENT=production",
			},
			ExposedPorts: map[string]struct{}{
				"8080/tcp": {},
			},
			StopSignal: "SIGTERM",
			Labels: map[string]string{
				"org.opencontainers.image.title":       "production-service-runtime",
				"org.opencontainers.image.description": "Production containerized workload and service runtime",
				"org.opencontainers.image.vendor":      "Infrastructure Team",
				"org.opencontainers.image.licenses":    "MIT",
			},
		},
		RootFS: RootFSBlock{
			Type:    "layers",
			DiffIDs: diffIDs,
		},
		History: historyList,
		Created: epochStr,
	}

	cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, "", 0, err
	}

	h := sha256.Sum256(cfgBytes)
	cfgDigest := "sha256:" + hex.EncodeToString(h[:])
	return cfgBytes, cfgDigest, int64(len(cfgBytes)), nil
}

// GenerateConfigJSON 生成默认 (amd64) 架构的 Config JSON (向前兼容)
func GenerateConfigJSON(layers []*TarLayer) ([]byte, string, int64, error) {
	return GenerateArchConfigJSON("amd64", layers)
}

// GenerateManifestJSON 组装指定配置与分层的 Manifest JSON
func GenerateManifestJSON(configDigest string, configSize int64, layers []*TarLayer, useDockerSchema bool) ([]byte, string, int64, error) {
	manifestMediaType := MediaTypeDockerManifestV2
	configMediaType := MediaTypeDockerConfigV1
	layerMediaType := MediaTypeDockerLayerTar

	if !useDockerSchema {
		manifestMediaType = MediaTypeOCIManifestV1
		configMediaType = MediaTypeOCIConfigV1
		layerMediaType = MediaTypeOCILayerTar
	}

	layerDescriptors := make([]Descriptor, 0, len(layers))
	for _, l := range layers {
		digest, err := l.ComputeDigest()
		if err != nil {
			return nil, "", 0, err
		}
		layerDescriptors = append(layerDescriptors, Descriptor{
			MediaType: layerMediaType,
			Size:      l.TotalSize,
			Digest:    digest,
		})
	}

	mf := Manifest{
		SchemaVersion: 2,
		MediaType:     manifestMediaType,
		Config: Descriptor{
			MediaType: configMediaType,
			Size:      configSize,
			Digest:    configDigest,
		},
		Layers: layerDescriptors,
	}

	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return nil, "", 0, err
	}

	h := sha256.Sum256(data)
	mfDigest := "sha256:" + hex.EncodeToString(h[:])
	return data, mfDigest, int64(len(data)), nil
}

// GenerateMultiArchIndex 组装多架构 Manifest List / OCI Image Index JSON
func GenerateMultiArchIndex(manifests []ManifestDescriptor, useDockerSchema bool) ([]byte, string, int64, error) {
	listMediaType := MediaTypeDockerManifestList
	if !useDockerSchema {
		listMediaType = MediaTypeOCIImageIndex
	}

	list := ManifestList{
		SchemaVersion: 2,
		MediaType:     listMediaType,
		Manifests:     manifests,
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return nil, "", 0, err
	}

	h := sha256.Sum256(data)
	listDigest := "sha256:" + hex.EncodeToString(h[:])
	return data, listDigest, int64(len(data)), nil
}
