package forwarding

import (
	"context"
	"time"

	loggingv1 "github.com/openshift/cluster-logging-operator/pkg/apis/logging/v1"
	logforwarding "github.com/openshift/cluster-logging-operator/pkg/apis/logging/v1alpha1"
	"github.com/openshift/cluster-logging-operator/pkg/constants"
	"github.com/openshift/cluster-logging-operator/pkg/k8shandler"
	"github.com/openshift/cluster-logging-operator/pkg/logger"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_forwarding")

const (
	singletonMessage = "LogForwarding is a singleton. Only an instance named 'instance' is allowed"
)

// Add creates a new Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileForwarding{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("logforwarding-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ClusterLogging
	err = c.Watch(&source.Kind{Type: &logforwarding.LogForwarding{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileForwarding{}

// ReconcileForwarding reconciles a LogForwarding object
type ReconcileForwarding struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

var (
	reconcilePeriod = 30 * time.Second
	reconcileResult = reconcile.Result{RequeueAfter: reconcilePeriod}
)

// Reconcile reads that state of the cluster for a LogForwarding object and makes changes based on the state read
// and what is in the LogForwarding.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileForwarding) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	logger.Debugf("logforwarding-controller Reconciling: %v", request)
	// Fetch the LogForwarding instance
	instance := &logforwarding.LogForwarding{}
	logger.Debug("logforwarding-controller fetching LF instance")
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		logger.Debugf("logforwarding-controller Error getting instance. It will be retried if other then 'NotFound': %v", err)
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	logger.Debugf("logforwarding-controller fetched LF instance: %v", instance)

	//check for instancename and then update status
	var reconcileErr error = nil
	if instance.Name != constants.SingletonName {
		instance.Status = logforwarding.NewForwardingStatus(logforwarding.LogForwardingStateRejected, logforwarding.LogForwardingReasonName, singletonMessage)
	} else {
		instance.Status = logforwarding.NewForwardingStatus(logforwarding.LogForwardingStateAccepted, logforwarding.LogForwardingReasonName, "")

		logger.Debug("logforwarding-controller fetching ClusterLogging instance...")
		clInstance := &loggingv1.ClusterLogging{}
		clName := types.NamespacedName{Name: constants.SingletonName, Namespace: constants.OpenshiftNS}
		err = r.client.Get(context.TODO(), clName, clInstance)
		if err != nil && !errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Debugf("logforwarding-controller error fetching ClusterLogging instance: %v")
			return reconcile.Result{}, err
		}

		logger.Debug("logforwarding-controller calling ClusterLogging reconciler...")
		reconcileErr = k8shandler.Reconcile(clInstance, instance, r.client)
	}

	logger.Debugf("logforwarding-controller updating status of instance: %v", instance)
	if err = r.client.Status().Update(context.TODO(), instance); err != nil {
		logger.Debugf("logforwarding-controller error updating status: %v", err)
		return reconcile.Result{}, err
	}

	logger.Debugf("logforwarding-controller returning %v, error: %v", reconcileResult, reconcileErr)
	return reconcileResult, reconcileErr
}
