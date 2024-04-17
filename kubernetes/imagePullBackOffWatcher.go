package kubernetes

import (
	"context"
	"time"

	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	_ "k8s.io/client-go/tools/cache"
)

const gracePeriod = 30 * time.Second

type imagePullBackOffWatcher struct {
	logger  *zap.Logger
	k8s     kubernetes.Interface
	failJob func()
}

// NewImagePullBackOffWatcher creates an informer that will fail jobs that have
// pods with containers in the ImagePullBackOff state
func NewImagePullBackOffWatcher(logger *zap.Logger) (*imagePullBackOffWatcher, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	k8s, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &imagePullBackOffWatcher{
		logger: logger,
		k8s:    k8s,
	}, nil
}

// Creates a Pods informer and registers the handler on it
func (w *imagePullBackOffWatcher) RegisterInformer(
	ctx context.Context,
	factory informers.SharedInformerFactory,
) error {
	informer := factory.Core().V1().Pods().Informer()
	if _, err := informer.AddEventHandler(w); err != nil {
		return err
	}
	go factory.Start(ctx.Done())
	return nil
}

func (w *imagePullBackOffWatcher) OnDelete(obj any) {}

func (w *imagePullBackOffWatcher) OnAdd(maybePod any, isInInitialList bool) {
	pod, wasPod := maybePod.(*v1.Pod)
	if !wasPod {
		return
	}

	w.failImagePullBackOff(pod)
}

func (w *imagePullBackOffWatcher) OnUpdate(oldMaybePod, newMaybePod any) {
	oldPod, oldWasPod := newMaybePod.(*v1.Pod)
	newPod, newWasPod := newMaybePod.(*v1.Pod)

	// This nonsense statement is only necessary because the types are too loose.
	// Most likely both old and new are going to be Pods.
	if newWasPod {
		w.failImagePullBackOff(newPod)
	} else if oldWasPod {
		w.failImagePullBackOff(oldPod)
	}
}

func (w *imagePullBackOffWatcher) failImagePullBackOff(pod *v1.Pod) {
	log := w.logger.With(zap.String("namespace", pod.Namespace), zap.String("podName", pod.Name))
	log.Debug("Checking pod for ImagePullBackOff")

	startedAt := pod.GetCreationTimestamp().Time
	if time.Since(startedAt) < gracePeriod {
		return
	}

	for _, containerStatus := range pod.Status.ContainerStatuses {
		if !shouldCancel(&containerStatus) {
			continue
		}

		log.Info("Job has a container in ImagePullBackOff. Failing.")

		w.failJob()
		return
	}
}

func shouldCancel(containerStatus *v1.ContainerStatus) bool {
	return containerStatus.State.Waiting != nil &&
		containerStatus.State.Waiting.Reason == "ImagePullBackOff"
}
