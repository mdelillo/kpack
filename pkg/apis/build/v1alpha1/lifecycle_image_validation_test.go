package v1alpha1

import (
	"testing"

	"github.com/sclevine/spec"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLifecycleImageValidator(t *testing.T) {
	spec.Run(t, "TestLifecycleImageValidator", testLifecycleImageValidator)
}

func testLifecycleImageValidator(t *testing.T, when spec.G, it spec.S) {
	var (
		validator LifecycleImageValidator
		cfg       *corev1.ConfigMap
	)
	it.Before(func() {
		validator = LifecycleImageValidator{
			SupportedPlatformApiVersions: []string{"0.3", "0.4"},
		}

		cfg = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      LifecycleConfigName,
				Namespace: LifecycleConfigNamespace,
				Annotations: map[string]string{
					LifecycleConfigPlatformApiVersionsAnnotation: "0.2 0.3",
				},
			},
		}
	})

	it("succeeds with at least one supported api version", func() {
		_, err := validator.Validate(cfg)
		require.NoError(t, err)
	})

	it("succeeds when platformApiVersions is not set", func() {
		cfg.Annotations = map[string]string{}
		_, err := validator.Validate(cfg)
		require.NoError(t, err)
	})

	it("fails with no supported api versions", func() {
		cfg.Annotations = map[string]string{
			"lifecycle.kpack.io/platformApiVersions": "0.10",
		}

		_, err := validator.Validate(cfg)
		require.EqualError(t, err, "lifecycle image does not contain a supported platform api version. supported versions: [0.3 0.4], lifecycle versions: [0.10]")
	})

	it("fails when the config name is unexpected", func() {
		cfg.Name = "invalid"

		_, err := validator.Validate(cfg)
		require.EqualError(t, err, "unexpected lifecycle image config name: invalid")
	})

	it("fails when the config namespace is unexpected", func() {
		cfg.Namespace = "invalid"

		_, err := validator.Validate(cfg)
		require.EqualError(t, err, "unexpected lifecycle image config namespace: invalid")
	})
}
