package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectPackageManagerApt(t *testing.T) {
	images := []string{
		"debian:latest",
		"debian:bullseye",
		"ubuntu:22.04",
		"ubuntu:latest",
		"golang:1.22",
		"golang:latest",
		"python:3.12",
		"python:3.12-slim",
		"node:20",
		"node:20-slim",
		"rust:1.75",
		"rust:latest",
	}

	for _, img := range images {
		t.Run(img, func(t *testing.T) {
			pm := DetectPackageManager(img)
			assert.Equal(t, PackageManagerApt, pm, "expected apt for image %s", img)
		})
	}
}

func TestDetectPackageManagerApk(t *testing.T) {
	images := []string{
		"alpine:latest",
		"alpine:3.19",
		"golang:1.22-alpine",
		"python:3.12-alpine",
		"node:20-alpine",
	}

	for _, img := range images {
		t.Run(img, func(t *testing.T) {
			pm := DetectPackageManager(img)
			assert.Equal(t, PackageManagerApk, pm, "expected apk for image %s", img)
		})
	}
}

func TestDetectPackageManagerYum(t *testing.T) {
	images := []string{
		"centos:7",
		"centos:latest",
		"amazonlinux:2",
		"amazonlinux:2023",
	}

	for _, img := range images {
		t.Run(img, func(t *testing.T) {
			pm := DetectPackageManager(img)
			assert.Equal(t, PackageManagerYum, pm, "expected yum for image %s", img)
		})
	}
}

func TestDetectPackageManagerDnf(t *testing.T) {
	images := []string{
		"fedora:39",
		"fedora:latest",
		"rockylinux:9",
		"rockylinux:latest",
		"almalinux:9",
		"almalinux:latest",
	}

	for _, img := range images {
		t.Run(img, func(t *testing.T) {
			pm := DetectPackageManager(img)
			assert.Equal(t, PackageManagerDnf, pm, "expected dnf for image %s", img)
		})
	}
}

func TestDetectPackageManagerUnknown(t *testing.T) {
	images := []string{
		"scratch",
		"busybox",
		"custom-image:latest",
		"myregistry.com/myimage:v1",
	}

	for _, img := range images {
		t.Run(img, func(t *testing.T) {
			pm := DetectPackageManager(img)
			assert.Equal(t, PackageManagerUnknown, pm, "expected unknown for image %s", img)
		})
	}
}

func TestGenerateInstallCommandApt(t *testing.T) {
	cmd := GenerateInstallCommand(PackageManagerApt, []string{"git", "curl", "make"})
	expected := "apt-get update && apt-get install -y --no-install-recommends git curl make && rm -rf /var/lib/apt/lists/*"
	assert.Equal(t, expected, cmd)
}

func TestGenerateInstallCommandApk(t *testing.T) {
	cmd := GenerateInstallCommand(PackageManagerApk, []string{"git", "curl", "make"})
	expected := "apk add --no-cache git curl make"
	assert.Equal(t, expected, cmd)
}

func TestGenerateInstallCommandYum(t *testing.T) {
	cmd := GenerateInstallCommand(PackageManagerYum, []string{"git", "curl", "make"})
	expected := "yum install -y git curl make && yum clean all"
	assert.Equal(t, expected, cmd)
}

func TestGenerateInstallCommandDnf(t *testing.T) {
	cmd := GenerateInstallCommand(PackageManagerDnf, []string{"git", "curl", "make"})
	expected := "dnf install -y git curl make && dnf clean all"
	assert.Equal(t, expected, cmd)
}

func TestGenerateInstallCommandUnknown(t *testing.T) {
	cmd := GenerateInstallCommand(PackageManagerUnknown, []string{"git", "curl"})
	assert.Empty(t, cmd)
}

func TestGenerateInstallCommandEmpty(t *testing.T) {
	cmd := GenerateInstallCommand(PackageManagerApt, []string{})
	assert.Empty(t, cmd)
}

func TestGenerateInstallCommandSingle(t *testing.T) {
	cmd := GenerateInstallCommand(PackageManagerApt, []string{"git"})
	expected := "apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/*"
	assert.Equal(t, expected, cmd)
}

func TestPackageManagerString(t *testing.T) {
	tests := []struct {
		pm       PackageManager
		expected string
	}{
		{PackageManagerApt, "apt"},
		{PackageManagerApk, "apk"},
		{PackageManagerYum, "yum"},
		{PackageManagerDnf, "dnf"},
		{PackageManagerUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.pm.String())
		})
	}
}

func TestDetectPackageManagerWithRegistry(t *testing.T) {
	// Should still detect from image name even with registry prefix
	tests := []struct {
		image    string
		expected PackageManager
	}{
		{"docker.io/library/ubuntu:22.04", PackageManagerApt},
		{"gcr.io/distroless/base-debian11", PackageManagerApt},
		{"quay.io/fedora/fedora:latest", PackageManagerDnf},
		{"registry.access.redhat.com/ubi8", PackageManagerDnf},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			pm := DetectPackageManager(tt.image)
			assert.Equal(t, tt.expected, pm)
		})
	}
}
