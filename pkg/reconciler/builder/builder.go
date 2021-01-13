package builder

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/controller"

	"github.com/pivotal/kpack/pkg/apis/build/v1alpha1"
	corev1alpha1 "github.com/pivotal/kpack/pkg/apis/core/v1alpha1"
	"github.com/pivotal/kpack/pkg/client/clientset/versioned"
	v1alpha1informers "github.com/pivotal/kpack/pkg/client/informers/externalversions/build/v1alpha1"
	v1alpha1Listers "github.com/pivotal/kpack/pkg/client/listers/build/v1alpha1"
	"github.com/pivotal/kpack/pkg/cnb"
	"github.com/pivotal/kpack/pkg/reconciler"
	"github.com/pivotal/kpack/pkg/registry"
	"github.com/pivotal/kpack/pkg/tracker"
)

const (
	ReconcilerName = "Builders"
	Kind           = "Builder"
)

type NewBuildpackRepository func(clusterStore *v1alpha1.ClusterStore) cnb.BuildpackRepository

type BuilderCreator interface {
	CreateBuilder(keychain authn.Keychain, lifecycleImage string, clusterStore *v1alpha1.ClusterStore, clusterStack *v1alpha1.ClusterStack, spec v1alpha1.BuilderSpec) (v1alpha1.BuilderRecord, error)
}

func NewController(opt reconciler.Options,
	builderInformer v1alpha1informers.BuilderInformer,
	builderCreator BuilderCreator,
	keychainFactory registry.KeychainFactory,
	cfgMapInformer corev1informers.ConfigMapInformer,
	clusterStoreInformer v1alpha1informers.ClusterStoreInformer,
	clusterStackInformer v1alpha1informers.ClusterStackInformer,
) *controller.Impl {
	c := &Reconciler{
		Client:             opt.Client,
		BuilderLister:      builderInformer.Lister(),
		BuilderCreator:     builderCreator,
		KeychainFactory:    keychainFactory,
		ConfigMapLister:    cfgMapInformer.Lister(),
		ClusterStoreLister: clusterStoreInformer.Lister(),
		ClusterStackLister: clusterStackInformer.Lister(),
	}
	impl := controller.NewImpl(c, opt.Logger, ReconcilerName)
	builderInformer.Informer().AddEventHandler(reconciler.Handler(impl.Enqueue))

	c.Tracker = tracker.New(impl.EnqueueKey, opt.TrackerResyncPeriod())
	clusterStoreInformer.Informer().AddEventHandler(reconciler.Handler(c.Tracker.OnChanged))
	clusterStackInformer.Informer().AddEventHandler(reconciler.Handler(c.Tracker.OnChanged))

	return impl
}

type Reconciler struct {
	Client             versioned.Interface
	BuilderLister      v1alpha1Listers.BuilderLister
	BuilderCreator     BuilderCreator
	KeychainFactory    registry.KeychainFactory
	Tracker            reconciler.Tracker
	ConfigMapLister    corev1listers.ConfigMapLister
	ClusterStoreLister v1alpha1Listers.ClusterStoreLister
	ClusterStackLister v1alpha1Listers.ClusterStackLister
}

func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	namespace, builderName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	builder, err := c.BuilderLister.Builders(namespace).Get(builderName)
	if k8serrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	builder = builder.DeepCopy()

	builderRecord, creationError := c.reconcileBuilder(builder)
	if creationError != nil {
		builder.Status.ErrorCreate(creationError)

		err := c.updateStatus(builder)
		if err != nil {
			return err
		}

		return controller.NewPermanentError(creationError)
	}

	builder.Status.BuilderRecord(builderRecord)
	return c.updateStatus(builder)
}

func (c *Reconciler) reconcileBuilder(builder *v1alpha1.Builder) (v1alpha1.BuilderRecord, error) {
	lifecycleCfg, err := c.ConfigMapLister.ConfigMaps(v1alpha1.LifecycleConfigNamespace).Get(v1alpha1.LifecycleConfigName)
	if err != nil {
		return v1alpha1.BuilderRecord{}, err
	}

	lifecycleImage, ok := lifecycleCfg.Data[v1alpha1.LifecycleConfigImageKey]
	if !ok {
		return v1alpha1.BuilderRecord{}, errors.New("invalid lifecycle image configuration")
	}

	err = c.Tracker.Track(lifecycleCfg, builder.NamespacedName())
	if err != nil {
		return v1alpha1.BuilderRecord{}, err
	}

	clusterStore, err := c.ClusterStoreLister.Get(builder.Spec.Store.Name)
	if err != nil {
		return v1alpha1.BuilderRecord{}, err
	}

	err = c.Tracker.Track(clusterStore, builder.NamespacedName())
	if err != nil {
		return v1alpha1.BuilderRecord{}, err
	}

	clusterStack, err := c.ClusterStackLister.Get(builder.Spec.Stack.Name)
	if err != nil {
		return v1alpha1.BuilderRecord{}, err
	}

	err = c.Tracker.Track(clusterStack, builder.NamespacedName())
	if err != nil {
		return v1alpha1.BuilderRecord{}, err
	}

	if !clusterStack.Status.GetCondition(corev1alpha1.ConditionReady).IsTrue() {
		return v1alpha1.BuilderRecord{}, errors.Errorf("stack %s is not ready", clusterStack.Name)
	}

	keychain, err := c.KeychainFactory.KeychainForSecretRef(registry.SecretRef{
		ServiceAccount: builder.Spec.ServiceAccount,
		Namespace:      builder.Namespace,
	})
	if err != nil {
		return v1alpha1.BuilderRecord{}, err
	}

	return c.BuilderCreator.CreateBuilder(keychain, lifecycleImage, clusterStore, clusterStack, builder.Spec.BuilderSpec)
}

func (c *Reconciler) updateStatus(desired *v1alpha1.Builder) error {
	desired.Status.ObservedGeneration = desired.Generation

	original, err := c.BuilderLister.Builders(desired.Namespace).Get(desired.Name)
	if err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(desired.Status, original.Status) {
		return nil
	}

	_, err = c.Client.KpackV1alpha1().Builders(desired.Namespace).UpdateStatus(desired)
	return err
}
