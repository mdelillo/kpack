package clusterBuilder_test

import (
	"errors"
	"testing"

	"github.com/sclevine/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"knative.dev/pkg/controller"
	rtesting "knative.dev/pkg/reconciler/testing"

	"github.com/pivotal/kpack/pkg/apis/build/v1alpha1"
	corev1alpha1 "github.com/pivotal/kpack/pkg/apis/core/v1alpha1"
	"github.com/pivotal/kpack/pkg/client/clientset/versioned/fake"
	"github.com/pivotal/kpack/pkg/reconciler/clusterbuilder"
	"github.com/pivotal/kpack/pkg/reconciler/testhelpers"
	"github.com/pivotal/kpack/pkg/registry"
	"github.com/pivotal/kpack/pkg/registry/registryfakes"
)

func TestClusterBuilderReconciler(t *testing.T) {
	spec.Run(t, "Custom Cluster Builder Reconciler", testClusterBuilderReconciler)
}

func testClusterBuilderReconciler(t *testing.T, when spec.G, it spec.S) {
	const (
		builderName             = "custom-builder"
		builderKey              = builderName
		builderTag              = "example.com/custom-builder"
		builderIdentifier       = "example.com/custom-builder@sha256:resolved-builder-digest"
		initialGeneration int64 = 1
	)

	var (
		builderCreator  = &testhelpers.FakeBuilderCreator{}
		keychainFactory = &registryfakes.FakeKeychainFactory{}
		fakeTracker     = testhelpers.FakeTracker{}
	)

	rt := testhelpers.ReconcilerTester(t,
		func(t *testing.T, row *rtesting.TableRow) (reconciler controller.Reconciler, lists rtesting.ActionRecorderList, list rtesting.EventList) {
			listers := testhelpers.NewListers(row.Objects)
			fakeClient := fake.NewSimpleClientset(listers.BuildServiceObjects()...)
			r := &clusterBuilder.Reconciler{
				Client:               fakeClient,
				ClusterBuilderLister: listers.GetClusterBuilderLister(),
				BuilderCreator:       builderCreator,
				KeychainFactory:      keychainFactory,
				Tracker:              fakeTracker,
				ConfigMapLister:      listers.GetConfigMapLister(),
				ClusterStoreLister:   listers.GetClusterStoreLister(),
				ClusterStackLister:   listers.GetClusterStackLister(),
			}
			return r, rtesting.ActionRecorderList{fakeClient}, rtesting.EventList{Recorder: record.NewFakeRecorder(10)}
		})

	lifecycleConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "lifecycle-image", Namespace: "kpack"},
		Data:       map[string]string{"image": "some-lifecycle-image"},
	}

	clusterStore := &v1alpha1.ClusterStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: "some-store",
		},
		Spec:   v1alpha1.ClusterStoreSpec{},
		Status: v1alpha1.ClusterStoreStatus{},
	}

	clusterStack := &v1alpha1.ClusterStack{
		ObjectMeta: metav1.ObjectMeta{
			Name: "some-stack",
		},
		Status: v1alpha1.ClusterStackStatus{
			Status: corev1alpha1.Status{
				ObservedGeneration: 0,
				Conditions: []corev1alpha1.Condition{
					{
						Type:   corev1alpha1.ConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		},
	}

	builder := &v1alpha1.ClusterBuilder{
		ObjectMeta: metav1.ObjectMeta{
			Name:       builderName,
			Generation: initialGeneration,
		},
		Spec: v1alpha1.ClusterBuilderSpec{
			BuilderSpec: v1alpha1.BuilderSpec{
				Tag: builderTag,
				Stack: corev1.ObjectReference{
					Kind: "Stack",
					Name: "some-stack",
				},
				Store: corev1.ObjectReference{
					Kind: "ClusterStore",
					Name: "some-store",
				},
				Order: []v1alpha1.OrderEntry{
					{
						Group: []v1alpha1.BuildpackRef{
							{
								BuildpackInfo: v1alpha1.BuildpackInfo{
									Id:      "buildpack.id.1",
									Version: "1.0.0",
								},
								Optional: false,
							},
							{
								BuildpackInfo: v1alpha1.BuildpackInfo{
									Id:      "buildpack.id.2",
									Version: "2.0.0",
								},
								Optional: false,
							},
						},
					},
				},
			},
			ServiceAccountRef: corev1.ObjectReference{
				Namespace: "some-sa-namespace",
				Name:      "some-sa-name",
			},
		},
	}

	secretRef := registry.SecretRef{
		ServiceAccount: builder.Spec.ServiceAccountRef.Name,
		Namespace:      builder.Spec.ServiceAccountRef.Namespace,
	}

	when("#Reconcile", func() {
		it.Before(func() {
			keychainFactory.AddKeychainForSecretRef(t, secretRef, &registryfakes.FakeKeychain{})
		})

		it("saves metadata to the status", func() {
			builderCreator.Record = v1alpha1.BuilderRecord{
				Image: builderIdentifier,
				Stack: v1alpha1.BuildStack{
					RunImage: "example.com/run-image@sha256:123456",
					ID:       "fake.stack.id",
				},
				Buildpacks: v1alpha1.BuildpackMetadataList{
					{
						Id:      "buildpack.id.1",
						Version: "1.0.0",
					},
					{
						Id:      "buildpack.id.2",
						Version: "2.0.0",
					},
				},
				ObservedStoreGeneration: 10,
				ObservedStackGeneration: 11,
			}

			expectedBuilder := &v1alpha1.ClusterBuilder{
				ObjectMeta: builder.ObjectMeta,
				Spec:       builder.Spec,
				Status: v1alpha1.BuilderStatus{
					Status: corev1alpha1.Status{
						ObservedGeneration: 1,
						Conditions: corev1alpha1.Conditions{
							{
								Type:   corev1alpha1.ConditionReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
					BuilderMetadata: []v1alpha1.BuildpackMetadata{
						{
							Id:      "buildpack.id.1",
							Version: "1.0.0",
						},
						{
							Id:      "buildpack.id.2",
							Version: "2.0.0",
						},
					},
					Stack: v1alpha1.BuildStack{
						RunImage: "example.com/run-image@sha256:123456",
						ID:       "fake.stack.id",
					},
					LatestImage:             builderIdentifier,
					ObservedStoreGeneration: 10,
					ObservedStackGeneration: 11,
				},
			}

			rt.Test(rtesting.TableRow{
				Key: builderKey,
				Objects: []runtime.Object{
					lifecycleConfig,
					clusterStack,
					clusterStore,
					builder,
				},
				WantErr: false,
				WantStatusUpdates: []clientgotesting.UpdateActionImpl{
					{
						Object: expectedBuilder,
					},
				},
			})

			assert.Equal(t, []testhelpers.CreateBuilderArgs{{
				Keychain:          &registryfakes.FakeKeychain{},
				LifecycleImageRef: "some-lifecycle-image",
				ClusterStore:      clusterStore,
				ClusterStack:      clusterStack,
				BuilderSpec:       builder.Spec.BuilderSpec,
			}}, builderCreator.CreateBuilderCalls)
		})

		it("tracks the stack and store for a custom builder", func() {
			builderCreator.Record = v1alpha1.BuilderRecord{
				Image: builderIdentifier,
				Stack: v1alpha1.BuildStack{
					RunImage: "example.com/run-image@sha256:123456",
					ID:       "fake.stack.id",
				},
				Buildpacks: v1alpha1.BuildpackMetadataList{},
			}

			expectedBuilder := &v1alpha1.ClusterBuilder{
				ObjectMeta: builder.ObjectMeta,
				Spec:       builder.Spec,
				Status: v1alpha1.BuilderStatus{
					Status: corev1alpha1.Status{
						ObservedGeneration: 1,
						Conditions: corev1alpha1.Conditions{
							{
								Type:   corev1alpha1.ConditionReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
					BuilderMetadata: []v1alpha1.BuildpackMetadata{},
					Stack: v1alpha1.BuildStack{
						RunImage: "example.com/run-image@sha256:123456",
						ID:       "fake.stack.id",
					},
					LatestImage: builderIdentifier,
				},
			}

			rt.Test(rtesting.TableRow{
				Key: builderKey,
				Objects: []runtime.Object{
					lifecycleConfig,
					clusterStack,
					clusterStore,
					expectedBuilder,
				},
				WantErr: false,
			})

			require.True(t, fakeTracker.IsTracking(lifecycleConfig, builder.NamespacedName()))
			require.True(t, fakeTracker.IsTracking(clusterStore, expectedBuilder.NamespacedName()))
			require.True(t, fakeTracker.IsTracking(clusterStack, builder.NamespacedName()))
		})

		it("does not update the status with no status change", func() {
			builderCreator.Record = v1alpha1.BuilderRecord{
				Image: builderIdentifier,
				Stack: v1alpha1.BuildStack{
					RunImage: "example.com/run-image@sha256:123456",
					ID:       "fake.stack.id",
				},
				Buildpacks: v1alpha1.BuildpackMetadataList{
					{
						Id:      "buildpack.id.1",
						Version: "1.0.0",
					},
				},
			}

			builder.Status = v1alpha1.BuilderStatus{
				Status: corev1alpha1.Status{
					ObservedGeneration: builder.Generation,
					Conditions: corev1alpha1.Conditions{
						{
							Type:   corev1alpha1.ConditionReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
				BuilderMetadata: []v1alpha1.BuildpackMetadata{
					{
						Id:      "buildpack.id.1",
						Version: "1.0.0",
					},
				},
				Stack: v1alpha1.BuildStack{
					RunImage: "example.com/run-image@sha256:123456",
					ID:       "fake.stack.id",
				},
				LatestImage: builderIdentifier,
			}

			rt.Test(rtesting.TableRow{
				Key: builderKey,
				Objects: []runtime.Object{
					lifecycleConfig,
					clusterStack,
					clusterStore,
					builder,
				},
				WantErr: false,
			})
		})

		it("updates status on creation error", func() {
			builderCreator.CreateErr = errors.New("create error")

			expectedBuilder := &v1alpha1.ClusterBuilder{
				ObjectMeta: builder.ObjectMeta,
				Spec:       builder.Spec,
				Status: v1alpha1.BuilderStatus{
					Status: corev1alpha1.Status{
						ObservedGeneration: 1,
						Conditions: corev1alpha1.Conditions{
							{
								Type:    corev1alpha1.ConditionReady,
								Status:  corev1.ConditionFalse,
								Message: "create error",
							},
						},
					},
				},
			}

			rt.Test(rtesting.TableRow{
				Key: builderKey,
				Objects: []runtime.Object{
					lifecycleConfig,
					clusterStack,
					clusterStore,
					builder,
				},
				WantErr: true,
				WantStatusUpdates: []clientgotesting.UpdateActionImpl{
					{
						Object: expectedBuilder,
					},
				},
			})
		})

		it("updates status and doesn't build builder when stack not ready", func() {
			notReadyClusterStack := &v1alpha1.ClusterStack{
				ObjectMeta: metav1.ObjectMeta{
					Name: "some-stack",
				},
				Status: v1alpha1.ClusterStackStatus{
					Status: corev1alpha1.Status{
						ObservedGeneration: 0,
						Conditions: []corev1alpha1.Condition{
							{
								Type:   corev1alpha1.ConditionReady,
								Status: corev1.ConditionFalse,
							},
						},
					},
				},
			}
			rt.Test(rtesting.TableRow{
				Key: builderKey,
				Objects: []runtime.Object{
					lifecycleConfig,
					notReadyClusterStack,
					clusterStore,
					builder,
				},
				WantErr: true,
				WantStatusUpdates: []clientgotesting.UpdateActionImpl{
					{
						Object: &v1alpha1.ClusterBuilder{
							ObjectMeta: builder.ObjectMeta,
							Spec:       builder.Spec,
							Status: v1alpha1.BuilderStatus{
								Status: corev1alpha1.Status{
									ObservedGeneration: 1,
									Conditions: corev1alpha1.Conditions{
										{
											Type:    corev1alpha1.ConditionReady,
											Status:  corev1.ConditionFalse,
											Message: "stack some-stack is not ready",
										},
									},
								},
							},
						},
					},
				},
			})

			//still track resources
			require.True(t, fakeTracker.IsTracking(lifecycleConfig, builder.NamespacedName()))
			require.True(t, fakeTracker.IsTracking(clusterStore, builder.NamespacedName()))
			require.True(t, fakeTracker.IsTracking(notReadyClusterStack, builder.NamespacedName()))
			require.Len(t, builderCreator.CreateBuilderCalls, 0)
		})

	})
}
