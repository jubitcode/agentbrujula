package build

import (
	"strings"
)

type PackageManager int

const (
	PackageManagerUnknown PackageManager = iota
	PackageManagerApt
	PackageManagerApk
	PackageManagerYum
	PackageManagerDnf
)

func (pm PackageManager) String() string {
	switch pm {
	case PackageManagerApt:
		return "apt"
	case PackageManagerApk:
		return "apk"
	case PackageManagerYum:
		return "yum"
	case PackageManagerDnf:
		return "dnf"
	default:
		return "unknown"
	}
}

var aptImages = []string{
	"debian",
	"ubuntu",
	"golang",
	"python",
	"node",
	"rust",
	"distroless/base-debian",
}

var apkImages = []string{
	"alpine",
}

var yumImages = []string{
	"centos",
	"amazonlinux",
}

var dnfImages = []string{
	"fedora",
	"rockylinux",
	"almalinux",
	"ubi",
}

func DetectPackageManager(image string) PackageManager {
	imageName := extractImageName(image)

	if strings.Contains(image, "-alpine") || strings.Contains(image, ":alpine") {
		return PackageManagerApk
	}

	for _, img := range apkImages {
		if strings.HasPrefix(imageName, img) {
			return PackageManagerApk
		}
	}

	for _, img := range aptImages {
		if strings.HasPrefix(imageName, img) || strings.Contains(imageName, img) {
			return PackageManagerApt
		}
	}

	for _, img := range yumImages {
		if strings.HasPrefix(imageName, img) {
			return PackageManagerYum
		}
	}

	for _, img := range dnfImages {
		if strings.HasPrefix(imageName, img) || strings.Contains(imageName, "/"+img) {
			return PackageManagerDnf
		}
	}

	return PackageManagerUnknown
}

func extractImageName(image string) string {
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		afterColon := image[idx+1:]
		if !strings.Contains(afterColon, "/") {
			image = image[:idx]
		}
	}

	if idx := strings.LastIndex(image, "/"); idx != -1 {
		image = image[idx+1:]
	}

	return image
}

func GenerateInstallCommand(pm PackageManager, packages []string) string {
	if len(packages) == 0 {
		return ""
	}

	pkgList := strings.Join(packages, " ")

	switch pm {
	case PackageManagerApt:
		return "apt-get update && apt-get install -y --no-install-recommends " + pkgList + " && rm -rf /var/lib/apt/lists/*"
	case PackageManagerApk:
		return "apk add --no-cache " + pkgList
	case PackageManagerYum:
		return "yum install -y " + pkgList + " && yum clean all"
	case PackageManagerDnf:
		return "dnf install -y " + pkgList + " && dnf clean all"
	default:
		return ""
	}
}
