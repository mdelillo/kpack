package v1alpha1

import (
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
)

const (
	LifecycleConfigName                          = "lifecycle-image"
	LifecycleConfigNamespace                     = "kpack"
	LifecycleConfigImageKey                      = "image"
	LifecycleConfigPlatformApiVersionsAnnotation = "lifecycle.kpack.io/platformApiVersions"
)

type LifecycleImageValidator struct {
	SupportedPlatformApiVersions []string
}

func (v LifecycleImageValidator) Validate(configMap *corev1.ConfigMap) (interface{}, error) {
	if configMap.Name != LifecycleConfigName {
		return nil, errors.Errorf("unexpected lifecycle image config name: %s", configMap.Name)
	}

	if configMap.Namespace != LifecycleConfigNamespace {
		return nil, errors.Errorf("unexpected lifecycle image config namespace: %s", configMap.Namespace)
	}

	platformApisVersions, ok := configMap.Annotations[LifecycleConfigPlatformApiVersionsAnnotation]
	if !ok {
		return nil, nil
	}
	apis := strings.Split(platformApisVersions, " ")

	for _, a := range apis {
		for _, sa := range v.SupportedPlatformApiVersions {
			if a == sa {
				return nil, nil
			}
		}
	}
	return nil, errors.Errorf("lifecycle image does not contain a supported platform api version. supported versions: %v, lifecycle versions: %v", v.SupportedPlatformApiVersions, apis)
}
