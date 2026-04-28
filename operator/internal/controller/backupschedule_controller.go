/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dumpscriptv1alpha1 "github.com/cloudscript-technology/dumpscript/operator/api/v1alpha1"
)

// BackupScheduleReconciler reconciles a BackupSchedule object.
type BackupScheduleReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=backupschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=backupschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dumpscript.cloudscript.com.br,resources=backupschedules/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch

// Reconcile materialises a BackupSchedule into a managed batch/v1 CronJob,
// reflecting suspend/schedule changes and propagating last-success / failure
// times from the latest Job runs back to the CR status.
func (r *BackupScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var bs dumpscriptv1alpha1.BackupSchedule
	if err := r.Get(ctx, req.NamespacedName, &bs); err != nil {
		// Resource gone — owner-ref garbage-collects the CronJob.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Build the desired CronJob and reconcile it.
	desired := buildCronJob(&bs)
	if err := controllerutil.SetControllerReference(&bs, desired, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("set controller ref: %w", err)
	}

	current := &batchv1.CronJob{}
	err := r.Get(ctx, client.ObjectKey{Name: desired.Name, Namespace: desired.Namespace}, current)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, desired); err != nil {
			return ctrl.Result{}, fmt.Errorf("create cronjob: %w", err)
		}
		log.Info("created cronjob", "name", desired.Name)
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get cronjob: %w", err)
	default:
		// Mutate in place — preserves resourceVersion for optimistic concurrency.
		current.Spec = desired.Spec
		current.Labels = desired.Labels
		if err := r.Update(ctx, current); err != nil {
			return ctrl.Result{}, fmt.Errorf("update cronjob: %w", err)
		}
		log.V(1).Info("updated cronjob", "name", desired.Name)
	}

	// Propagate last-success / last-failure from the most recent Job runs.
	if err := r.refreshStatus(ctx, &bs); err != nil {
		log.Error(err, "refresh status")
	}
	return ctrl.Result{}, nil
}

// refreshStatus walks the Jobs owned by the managed CronJob and populates
// LastScheduleTime / LastSuccessTime / LastFailureTime + CurrentRun.
func (r *BackupScheduleReconciler) refreshStatus(ctx context.Context, bs *dumpscriptv1alpha1.BackupSchedule) error {
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs,
		client.InNamespace(bs.Namespace),
		client.MatchingLabels{"dumpscript.cloudscript.com.br/schedule": bs.Name},
	); err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}

	bs.Status.CurrentRun = ""
	for i := range jobs.Items {
		j := &jobs.Items[i]
		switch {
		case j.Status.CompletionTime != nil &&
			(bs.Status.LastSuccessTime == nil || j.Status.CompletionTime.After(bs.Status.LastSuccessTime.Time)):
			bs.Status.LastSuccessTime = j.Status.CompletionTime
		case j.Status.Failed > 0 &&
			(bs.Status.LastFailureTime == nil || j.CreationTimestamp.After(bs.Status.LastFailureTime.Time)):
			t := j.CreationTimestamp
			bs.Status.LastFailureTime = &t
		case j.Status.Active > 0:
			bs.Status.CurrentRun = j.Name
		}
		if bs.Status.LastScheduleTime == nil ||
			j.CreationTimestamp.After(bs.Status.LastScheduleTime.Time) {
			t := j.CreationTimestamp
			bs.Status.LastScheduleTime = &t
		}
	}
	return r.Status().Update(ctx, bs)
}

// SetupWithManager sets up the controller with the Manager.
// Owns CronJobs so spec/status changes re-trigger Reconcile.
// Also watches Jobs labelled with the schedule name so that Job
// completion events update LastSuccessTime / LastFailureTime promptly.
func (r *BackupScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dumpscriptv1alpha1.BackupSchedule{}).
		Owns(&batchv1.CronJob{}).
		Watches(
			&batchv1.Job{},
			handler.EnqueueRequestsFromMapFunc(r.jobToSchedule),
		).
		Named("backupschedule").
		Complete(r)
}

// jobToSchedule maps a Job back to its owning BackupSchedule using the
// "dumpscript.cloudscript.com.br/schedule" label set by buildCronJob.
func (r *BackupScheduleReconciler) jobToSchedule(_ context.Context, obj client.Object) []reconcile.Request {
	name, ok := obj.GetLabels()["dumpscript.cloudscript.com.br/schedule"]
	if !ok {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Name: name, Namespace: obj.GetNamespace()},
	}}
}
