// Copyright Red Hat

package installer

import (
	"context"
	"fmt"
	"os"

	// "fmt"
	// "os"

	"github.com/ghodss/yaml"
	giterrors "github.com/pkg/errors"

	admissionregistration "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	// "sigs.k8s.io/controller-runtime/pkg/handler"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	// "sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	"github.com/stolostron/cluster-registration-operator/pkg/helpers"

	singaporev1alpha1 "github.com/stolostron/cluster-registration-operator/api/singapore/v1alpha1"
	clusterregistrarconfig "github.com/stolostron/cluster-registration-operator/config"
	"github.com/stolostron/cluster-registration-operator/deploy"
	clusteradmapply "open-cluster-management.io/clusteradm/pkg/helpers/apply"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	//+kubebuilder:scaffold:imports
)

// ClusterRegistrarReconciler reconciles a Strategy object
type ClusterRegistrarReconciler struct {
	client.Client
	KubeClient         kubernetes.Interface
	DynamicClient      dynamic.Interface
	APIExtensionClient apiextensionsclient.Interface
	Log                logr.Logger
	Scheme             *runtime.Scheme
}

var podName, podNamespace string

// +kubebuilder:rbac:groups="",resources={namespaces, pods},verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources={services,serviceaccounts,configmaps},verbs=get;create;update;list;watch;delete

// +kubebuilder:rbac:groups="apps",resources={deployments},verbs=get;create;update;list;watch;delete

// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources={clusterroles},verbs=escalate;get;create;update;delete;bind;list
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources={clusterrolebindings},verbs=get;create;update;delete;list;watch
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources={roles},verbs=get;create;update;delete;escalate;bind;list;watch
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources={rolebindings},verbs=get;create;update;delete;list;watch

// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources={customresourcedefinitions},verbs=get;create;update;delete

// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources={validatingwebhookconfigurations},verbs=get;create;update;list;watch;delete
// +kubebuilder:rbac:groups="apiregistration.k8s.io",resources={apiservices},verbs=get;create;update;list;watch;delete

// +kubebuilder:rbac:groups="singapore.open-cluster-management.io",resources={clusterregistrars},verbs=get;create;update;list;watch;delete

// +kubebuilder:rbac:groups="multicluster.openshift.io",resources={multiclusterengines},verbs=get;list;watch
// +kubebuilder:rbac:groups="operator.open-cluster-management.io",resources={multiclusterhubs},verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Strategy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *ClusterRegistrarReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("namespace", req.NamespacedName, "name", req.Name)

	// your logic here
	instance := &singaporev1alpha1.ClusterRegistrar{}

	if err := r.Client.Get(
		context.TODO(),
		types.NamespacedName{Namespace: req.Namespace, Name: req.Name},
		instance,
	); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	r.Log.Info("Instance", "instance", instance)
	r.Log.Info("Running Reconcile for Cluster Registrar", "Name: ", instance.GetName(), " Namespace:", instance.GetNamespace())

	if instance.DeletionTimestamp != nil {
		if err := r.processClusterRegistrarDeletion(instance); err != nil {
			return reconcile.Result{}, err
		}
		r.Log.Info("remove finalizer", "Finalizer:", helpers.ClusterRegistrarFinalizer, "name", instance.Name, "namespace", instance.Namespace)
		controllerutil.RemoveFinalizer(instance, helpers.ClusterRegistrarFinalizer)
		if err := r.Client.Update(context.TODO(), instance); err != nil {
			return ctrl.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// Add finalizer on clusterregistrar to make sure the installer process it.
	controllerutil.AddFinalizer(instance, helpers.ClusterRegistrarFinalizer)

	if err := r.Client.Update(context.TODO(), instance); err != nil {
		return ctrl.Result{}, giterrors.WithStack(err)
	}

	if err := r.processClusterRegistrarCreation(instance); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ClusterRegistrarReconciler) processClusterRegistrarCreation(clusterRegistrar *singaporev1alpha1.ClusterRegistrar) error {
	r.Log.Info("processClusterRegistrarCreation", "Name", clusterRegistrar.Name, "Namespace", clusterRegistrar.Namespace)
	pod := &corev1.Pod{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: podName, Namespace: podNamespace}, pod); err != nil {
		return err
	}
	r.Log.Info("Pod", "Name", pod.Name, "Namespace", pod.Namespace, "deletiontimeStamp", pod.DeletionTimestamp)
	applierBuilder := &clusteradmapply.ApplierBuilder{}
	applier := applierBuilder.WithClient(r.KubeClient, r.APIExtensionClient, r.DynamicClient).Build()
	readerDeploy := deploy.GetScenarioResourcesReader()

	//Deploy dex operator
	files := []string{
		"cluster-registration-operator/service_account.yaml",
		"cluster-registration-operator/leader_election_role.yaml",
		"cluster-registration-operator/leader_election_role_binding.yaml",
		"cluster-registration-operator/clusterrole.yaml",
		"cluster-registration-operator/clusterrole_binding.yaml",
	}

	image := pod.Spec.Containers[0].Image
	values := struct {
		Image     string
		Namespace string
	}{
		Image:     image,
		Namespace: podNamespace,
	}

	_, err := applier.ApplyDirectly(readerDeploy, values, false, "", files...)
	if err != nil {
		return giterrors.WithStack(err)
	}

	files = []string{
		"cluster-registration-operator/manager.yaml",
	}

	_, err = applier.ApplyDeployments(readerDeploy, values, false, "", files...)
	if err != nil {
		return giterrors.WithStack(err)
	}

	//Deploy webhook
	files = []string{
		"webhook/service_account.yaml",
		"webhook/webhook_clusterrole.yaml",
		"webhook/webhook_clusterrolebinding.yaml",
		"webhook/webhook_service.yaml",
	}

	_, err = applier.ApplyDirectly(readerDeploy, values, false, "", files...)
	if err != nil {
		return giterrors.WithStack(err)
	}

	files = []string{
		"webhook/webhook.yaml",
	}

	_, err = applier.ApplyDeployments(readerDeploy, values, false, "", files...)
	if err != nil {
		return giterrors.WithStack(err)
	}

	b, err := applier.MustTemplateAsset(readerDeploy, values, "", "webhook/webhook_validating_config.yaml")
	if err != nil {
		return giterrors.WithStack(err)
	}

	validationWebhookConfiguration := &admissionregistration.ValidatingWebhookConfiguration{}
	err = yaml.Unmarshal(b, validationWebhookConfiguration)
	if err != nil {
		return giterrors.WithStack(err)
	}

	if err := r.Client.Create(context.TODO(), validationWebhookConfiguration, &client.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			return giterrors.WithStack(err)
		}
	}

	b, err = applier.MustTemplateAsset(readerDeploy, values, "", "webhook/webhook_apiservice.yaml")
	if err != nil {
		return giterrors.WithStack(err)
	}

	apiService := &apiregistrationv1.APIService{}
	err = yaml.Unmarshal(b, apiService)
	if err != nil {
		return giterrors.WithStack(err)
	}
	if err := r.Client.Create(context.TODO(), apiService, &client.CreateOptions{}); err != nil {
		if !errors.IsAlreadyExists(err) {
			return giterrors.WithStack(err)
		}
	}

	return nil
}

func (r *ClusterRegistrarReconciler) processClusterRegistrarDeletion(clusterRegistrar *singaporev1alpha1.ClusterRegistrar) error {
	r.Log.Info("processClusterRegistrarDeletion", "Name", clusterRegistrar.Name, "Namespace", clusterRegistrar.Namespace)
	//Delete operator deployment
	r.Log.Info("Delete deployment", "name", "cluster-registration-operator-manager", "namespace", podNamespace)
	clusterRegOperatorDeployment := &appsv1.Deployment{}
	err := r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-operator-manager", Namespace: podNamespace}, clusterRegOperatorDeployment)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), clusterRegOperatorDeployment, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete roleBinding", "name", "cluster-registration-operator-leader-election-rolebinding", "namespace", podNamespace)
	clusterRegOperatorLeaderElectionRoleBinding := &rbacv1.RoleBinding{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-operator-leader-election-rolebinding", Namespace: podNamespace}, clusterRegOperatorLeaderElectionRoleBinding)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), clusterRegOperatorLeaderElectionRoleBinding, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete ClusterRoleBinding", "name", "cluster-registration-operator-manager-rolebinding", "namespace", podNamespace)
	clusterRegOperatorClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-operator-manager-rolebinding", Namespace: podNamespace}, clusterRegOperatorClusterRoleBinding)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), clusterRegOperatorClusterRoleBinding, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete serviceAccount", "name", "cluster-registration-operator-manager", "namespace", podNamespace)
	clusterRegOperatorServiceAccount := &corev1.ServiceAccount{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-operator-manager", Namespace: podNamespace}, clusterRegOperatorServiceAccount)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), clusterRegOperatorServiceAccount, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete ClusterRole", "name", "cluster-registration-operator-manager-role", "namespace", podNamespace)
	clusterRegOperatorClusterRole := &rbacv1.ClusterRole{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-operator-manager-role"}, clusterRegOperatorClusterRole)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), clusterRegOperatorClusterRole, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete Role", "name", "leader-election-operator-role", "namespace", podNamespace)
	clusterRegOperatorRole := &rbacv1.Role{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "leader-election-operator-role", Namespace: podNamespace}, clusterRegOperatorRole)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), clusterRegOperatorRole, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	// // Do not delete webhook on functional test as it is not installed
	// pod := &corev1.Pod{}
	// if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: podName, Namespace: podNamespace}, pod); err != nil {
	// 	return err
	// }
	// r.Log.Info("Pod", "Name", pod.Name, "Namespace", pod.Namespace, "deletiontimeStamp", pod.DeletionTimestamp)
	// if strings.Contains(pod.Spec.Containers[0].Image, "coverage") {
	// 	return nil
	// }

	//Delete webhook
	r.Log.Info("Delete Deployment", "name", "cluster-registration-webhook-service", "namespace", podNamespace)
	webhookDeployment := &appsv1.Deployment{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-webhook-service", Namespace: podNamespace}, webhookDeployment)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), webhookDeployment, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete APIService", "name", "v1alpha1.admission.singapore.open-cluster-management.io")
	apiService := &apiregistrationv1.APIService{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "v1alpha1.admission.singapore.open-cluster-management.io"}, apiService)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), apiService, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete ClusterRoleBinding", "name", "cluster-registration-webhook-service")
	webHookClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-webhook-service"}, webHookClusterRoleBinding)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), webHookClusterRoleBinding, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete ClusterRole", "name", "cluster-registration-webhook-service")
	webHookClusterRole := &rbacv1.ClusterRole{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-webhook-service"}, webHookClusterRole)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), webHookClusterRole, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete serviceAccount", "name", "cluster-registration-webhook-service", "namespace", podNamespace)
	webHookServiceAccount := &corev1.ServiceAccount{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-webhook-service", Namespace: podNamespace}, webHookServiceAccount)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), webHookServiceAccount, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete Service", "name", "cluster-registration-webhook-service", "namespace", podNamespace)
	service := &corev1.Service{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-webhook-service", Namespace: podNamespace}, service)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), service, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	r.Log.Info("Delete ValidatingWebhookConfiguration", "name", "cluster-registration-webhook-service", "namespace", podNamespace)
	validationWebhook := &admissionregistration.ValidatingWebhookConfiguration{}
	err = r.Client.Get(context.TODO(), client.ObjectKey{Name: "cluster-registration-webhook-service", Namespace: podNamespace}, validationWebhook)
	switch {
	case errors.IsNotFound(err):
	case err == nil:
		if err := r.Client.Delete(context.TODO(), validationWebhook, &client.DeleteOptions{}); err != nil {
			return giterrors.WithStack(err)
		}
	default:
		return giterrors.WithStack(err)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterRegistrarReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Log.Info("setup installer manager")
	if err := singaporev1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return giterrors.WithStack(err)
	}

	if err := apiregistrationv1.AddToScheme(mgr.GetScheme()); err != nil {
		return giterrors.WithStack(err)
	}

	if err := admissionregistration.AddToScheme(mgr.GetScheme()); err != nil {
		return giterrors.WithStack(err)
	}

	//Install CRD
	applierBuilder := &clusteradmapply.ApplierBuilder{}
	applier := applierBuilder.WithClient(r.KubeClient, r.APIExtensionClient, r.DynamicClient).Build()

	readerClusterRegOperator := clusterregistrarconfig.GetScenarioResourcesReader()

	files := []string{
		"crd/singapore.open-cluster-management.io_clusterregistrars.yaml",
		"crd/singapore.open-cluster-management.io_registeredclusters.yaml",
		"crd/singapore.open-cluster-management.io_hubconfigs.yaml",
	}
	if _, err := applier.ApplyDirectly(readerClusterRegOperator, nil, false, "", files...); err != nil {
		return giterrors.WithStack(err)
	}

	podName = os.Getenv("POD_NAME")
	podNamespace = os.Getenv("POD_NAMESPACE")
	if len(podName) == 0 || len(podNamespace) == 0 {
		return fmt.Errorf("POD_NAME or POD_NAMESPACE not set")
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&singaporev1alpha1.ClusterRegistrar{}).
		Complete(r)
}
